import type { MockRequest, MockResult, MockRoute } from '../adapter'
import { seedNudges } from './team'
import type { Newcomer, SiteCode, TimelineEvent } from '@/types/api'
import { SITES } from '@/stores/site'
import {
  buildTimeline,
  ensureSeeded,
  fireAt,
  inviteLink,
  newcomers,
  newId,
  scenarios,
  timelines,
  users,
  view,
  zoneOf,
} from '../state'
import { currentUser, forbidden } from './auth'
import { isValidTz } from '@/lib/tz'

// Mirrors the real API (backend/handlers, api_contract.md v1 + backend note):
// the same statuses and codes — 422 VALIDATION_FAILED with details[], 404
// NOT_FOUND, 409 CONFLICT / scenario_not_published — so a screen that works
// on mocks behaves the same against the server.

const DATE_RE = /^\d{4}-\d{2}-\d{2}$/
const PHONE_RE = /^\+?[\d\s()-]{10,20}$/
const MIN_TRAVEL_DAY = -30
const MAX_TRAVEL_DAY = 120

interface FieldError {
  field: string
  code: string
  message: string
}

function error(status: number, code: string, message: string, details?: FieldError[]): MockResult {
  return {
    status,
    data: { error: { code, message, traceId: `mock-${Date.now()}`, details } },
  }
}

function invalid(details: FieldError[]): MockResult {
  return error(422, 'VALIDATION_FAILED', 'Проверьте данные запроса', details)
}

function str(v: unknown): string {
  return typeof v === 'string' ? v.trim() : ''
}

function notFound(): MockResult {
  return error(404, 'NOT_FOUND', 'Сотрудник не найден')
}

function list(req: MockRequest): MockResult {
  const { site, q, risk, mine, recruiter_id } = req.query
  const needle = (q ?? '').trim().toLowerCase()
  const me = currentUser()
  // A recruiter sees only his own, with or without ?mine=1.
  const own = me && (mine === '1' || me.role === 'recruiter') ? me.id : null
  const items = [...newcomers.values()]
    .map(view)
    .filter((n) => !own || n.recruiter_id === own)
    .filter(
      (n) =>
        !recruiter_id ||
        (recruiter_id === 'none' ? !n.recruiter_id : n.recruiter_id === recruiter_id),
    )
    .filter((n) => !site || n.site === site)
    .filter((n) => !risk || n.risk === risk)
    .filter(
      (n) =>
        !needle ||
        [n.full_name, n.department, n.position, n.manager_name, n.mentor_name].some((f) =>
          f.toLowerCase().includes(needle),
        ),
    )
  return { status: 200, data: { items, total: items.length } }
}

function create(req: MockRequest): MockResult {
  const b = req.body
  const full_name = str(b.full_name)
  const phone = str(b.phone)
  const site = str(b.site) as SiteCode
  const start_date = str(b.start_date)
  const shift = b.shift === null || b.shift === undefined ? '' : String(b.shift).trim()
  const details: FieldError[] = []
  if (!full_name)
    details.push({ field: 'full_name', code: 'REQUIRED', message: 'Обязательное поле' })
  if (phone && !PHONE_RE.test(phone))
    details.push({ field: 'phone', code: 'INVALID_FORMAT', message: 'Телефон: +7 и 10 цифр' })
  if (!(SITES as readonly string[]).includes(site))
    details.push({
      field: 'site',
      code: 'OUT_OF_RANGE',
      message: 'Площадка: GrPP, Luzhniki или GTP',
    })
  if (!DATE_RE.test(start_date))
    details.push({
      field: 'start_date',
      code: 'INVALID_FORMAT',
      message: 'Дата в формате YYYY-MM-DD',
    })
  if (shift && !['1', '2', '3'].includes(shift))
    details.push({ field: 'shift', code: 'OUT_OF_RANGE', message: 'Смена: 1, 2, 3 или null' })
  if (details.length > 0) return invalid(details)

  const scenario = scenarios.get(str(b.scenario_id))
  if (!scenario)
    return error(422, 'VALIDATION_FAILED', 'Сценарий не найден', [
      { field: 'scenario_id', code: 'INVALID_FORMAT', message: 'Сценарий не найден' },
    ])
  // Launch takes the last published version; a never-published draft has none.
  if (scenario.version === 0)
    return error(
      409,
      'scenario_not_published',
      'Сценарий ещё не опубликован — запустить по нему адаптацию нельзя',
    )
  if (scenario.sites.length > 0 && !scenario.sites.includes(site))
    return error(422, 'VALIDATION_FAILED', 'Сценарий не для этой площадки', [
      { field: 'scenario_id', code: 'OUT_OF_RANGE', message: 'Сценарий не для этой площадки' },
    ])

  const me = currentUser()
  const recruiterId =
    typeof b.recruiter_id === 'string' && users.has(b.recruiter_id)
      ? b.recruiter_id
      : me?.role === 'recruiter'
        ? me.id
        : null
  const tz = typeof b.tz === 'string' && isValidTz(b.tz) ? b.tz : null
  const stored: Omit<Newcomer, 'day' | 'stage'> = {
    id: newId(),
    recruiter_id: recruiterId,
    tz,
    full_name,
    phone,
    site,
    department: str(b.department),
    position: str(b.position),
    manager_name: str(b.manager_name),
    mentor_name: str(b.mentor_name),
    recruiter_name: str(b.recruiter_name),
    start_date,
    shift: shift === '1' || shift === '2' || shift === '3' ? shift : null,
    scenario_id: scenario.id,
    scenario_version: scenario.version,
    address_form: null,
    consent: null,
    connection: 'invited',
    invite_link: inviteLink(),
    last_activity_at: null,
    last_pulse: null,
    risk: 'green',
    risk_reason: null,
    open_tickets: 0,
  }
  newcomers.set(stored.id, stored)
  timelines.set(stored.id, buildTimeline(stored, scenario, null))
  return { status: 201, data: view(stored) }
}

// PATCH {"start_date"?, "tz"?, "recruiter_id"?} (v5: every field optional):
// unsent events move with the date or the zone, sent ones stay.
function patch(req: MockRequest): MockResult {
  const n = newcomers.get(req.params.id)
  if (!n) return notFound()
  const b = req.body
  if ('start_date' in b) {
    const start_date = str(b.start_date)
    if (!DATE_RE.test(start_date))
      return invalid([
        { field: 'start_date', code: 'INVALID_FORMAT', message: 'Дата в формате YYYY-MM-DD' },
      ])
    n.start_date = start_date
  }
  if ('tz' in b) {
    if (b.tz !== null && (typeof b.tz !== 'string' || !isValidTz(b.tz)))
      return invalid([{ field: 'tz', code: 'INVALID_FORMAT', message: 'Неизвестный пояс' }])
    n.tz = b.tz
  }
  if ('recruiter_id' in b) {
    const me = currentUser()
    if (me && me.role !== 'hr_lead') return forbidden()
    const id = b.recruiter_id
    if (id !== null && (typeof id !== 'string' || !users.has(id)))
      return invalid([{ field: 'recruiter_id', code: 'NOT_FOUND', message: 'Рекрутер не найден' }])
    n.recruiter_id = id
    n.recruiter_name = id ? (users.get(id)?.full_name ?? '') : ''
  }
  const steps = scenarios.get(n.scenario_id)?.steps ?? []
  for (const e of timelines.get(n.id) ?? []) {
    if (e.status !== 'scheduled') continue
    const step = steps.find((s) => s.id === e.step_id)
    if (step) e.fire_at = fireAt(n.start_date, step, zoneOf(n))
  }
  return { status: 200, data: view(n) }
}

// The text of a step as the bot would send it, in the newcomer's address form.
function sentText(n: Omit<Newcomer, 'day' | 'stage'>, e: TimelineEvent, mark: boolean): string {
  const step = scenarios.get(n.scenario_id)?.steps.find((s) => s.id === e.step_id)
  const text = (n.address_form === 'ty' ? step?.text_ty : step?.text_vy) || e.title
  return mark ? `[ТЕСТ] ${text}` : text
}

// Sends one event: without consent surveys stay scheduled (the real bot too).
function send(n: Omit<Newcomer, 'day' | 'stage'>, e: TimelineEvent, mark: boolean): void {
  if (e.status !== 'scheduled') return
  if (e.kind === 'survey' && n.consent !== 'given') return
  e.status = 'sent'
  e.text = sentText(n, e, mark)
}

function timeTravel(req: MockRequest): MockResult {
  const n = newcomers.get(req.params.id)
  const events = timelines.get(req.params.id)
  if (!n || !events) return notFound()
  const day = req.body.day
  if (day === undefined || day === null)
    return invalid([{ field: 'day', code: 'REQUIRED', message: 'Обязательное поле' }])
  if (typeof day !== 'number' || !Number.isInteger(day))
    return error(422, 'VALIDATION_FAILED', 'Тело запроса — не JSON нужной формы')
  if (day < MIN_TRAVEL_DAY || day > MAX_TRAVEL_DAY)
    return invalid([
      {
        field: 'day',
        code: 'OUT_OF_RANGE',
        message: `День от ${MIN_TRAVEL_DAY} до ${MAX_TRAVEL_DAY}`,
      },
    ])
  if (n.connection !== 'connected')
    return error(409, 'CONFLICT', 'Сотрудник ещё не подключился к боту — отправлять некому')
  for (const e of events) if (e.day_offset <= day) send(n, e, true)
  return { status: 200, data: { items: events.map((e) => ({ ...e })) } }
}

// POST /dev/simulate-start (development only on the server too): the newcomer
// opens the invite and, optionally, answers the greeting and the consent.
function simulateStart(req: MockRequest): MockResult {
  const n = newcomers.get(str(req.body.newcomer_id))
  if (!n) return notFound()
  n.connection = 'connected'
  n.last_activity_at = new Date().toISOString()
  const form = req.body.address_form
  if (form === 'ty' || form === 'vy') n.address_form = form
  const consent = req.body.consent
  if (consent === 'given' || consent === 'declined') {
    n.consent = consent
    // After the consent the bot sends what is already due, without [ТЕСТ].
    const now = Date.now()
    for (const e of timelines.get(n.id) ?? []) {
      if (consent === 'declined' && e.kind === 'survey' && e.status === 'scheduled')
        e.status = 'skipped'
      else if (Date.parse(e.fire_at) <= now) send(n, e, false)
    }
  }
  return { status: 200, data: view(n) }
}

const routes: MockRoute[] = [
  { method: 'GET', path: '/newcomers', handler: list },
  { method: 'POST', path: '/newcomers', handler: create },
  {
    method: 'GET',
    path: '/newcomers/:id',
    handler: (req) => {
      const n = newcomers.get(req.params.id)
      return n ? { status: 200, data: view(n) } : notFound()
    },
  },
  { method: 'PATCH', path: '/newcomers/:id', handler: patch },
  {
    method: 'GET',
    path: '/newcomers/:id/timeline',
    handler: (req) => {
      const events = timelines.get(req.params.id)
      return events ? { status: 200, data: { items: events.map((e) => ({ ...e })) } } : notFound()
    },
  },
  { method: 'POST', path: '/newcomers/:id/time-travel', handler: timeTravel },
  { method: 'POST', path: '/dev/simulate-start', handler: simulateStart },
]

export const newcomerRoutes: MockRoute[] = routes.map((r) => ({
  ...r,
  handler: (req) => {
    ensureSeeded()
    seedNudges()
    return r.handler(req)
  },
}))
