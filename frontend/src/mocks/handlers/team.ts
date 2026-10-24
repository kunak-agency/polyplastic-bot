import type { MockRequest, MockResult, MockRoute } from '../adapter'
import {
  ensureSeeded,
  newcomers,
  publicUser,
  tickets,
  timelines,
  users,
  view,
  zoneOf,
} from '../state'
import { currentUser, forbidden } from './auth'
import type {
  DashTile,
  DashboardItem,
  HrLeadDashboard,
  IpDecision,
  LoadRow,
  Newcomer,
  NudgeItem,
  RecruiterDashboard,
  Role,
  TeamMember,
  TodayActionId,
  TodayRow,
  Zone,
} from '@/types/api'
import { addDays, localIsoDate } from '@/lib/calendar'
import { utcOffsetMinutes } from '@/lib/tz'

// Recruiter's dashboard, team, users and reassignment (api_contract_auth.md).

// Facts the mock has no source for yet (catch-up queue, zone v2), per seeded
// newcomer, so every row type, tile and chip of the demo has someone under it.
const EXTRA: Record<string, { catchup?: { done: number; total: number }; behind?: number }> = {
  '3f0c1a7e-5b2d-4c8e-a1f0-000000000004': { catchup: { done: 1, total: 4 }, behind: 2 },
}

// "Mark: contacted" hides the silent row (until reload of the page).
const contacted = new Set<string>()
let botNudges = 7
const BOT_SUMMARIES = 2
const SILENT_AFTER = 3
const SILENT_RED = 5
const LOW_PULSE = 3

function err(status: number, code: string, message: string): MockResult {
  return { status, data: { error: { code, message, traceId: `mock-${Date.now()}` } } }
}

function isLead(): boolean {
  // With auth off the mock lets everything through, like the server would
  // without the role middleware.
  const me = currentUser()
  return !me || me.role === 'hr_lead'
}

function silentDays(n: Newcomer): number | null {
  if (n.connection !== 'connected' || !n.last_activity_at) return null
  const days = Math.floor((Date.now() - Date.parse(n.last_activity_at)) / 86_400_000)
  return days >= SILENT_AFTER ? days : null
}

function weekBounds(): [string, string] {
  const now = new Date()
  const monday = new Date(now.getFullYear(), now.getMonth(), now.getDate())
  monday.setDate(monday.getDate() - ((monday.getDay() + 6) % 7))
  const from = localIsoDate(monday)
  return [from, addDays(from, 6)]
}

interface TicketFacts {
  open: number
  sla: number
  expiring: number
  first: { id: string; due: string | null; unassigned: boolean; overdue: boolean } | null
}

function ticketsOf(id: string): TicketFacts {
  const out: TicketFacts = { open: 0, sla: 0, expiring: 0, first: null }
  const endOfDay = new Date()
  endOfDay.setHours(23, 59, 59, 999)
  for (const t of tickets.values()) {
    if (t.newcomer_id !== id || t.status === 'resolved') continue
    out.open++
    const due = t.sla_due_at ? Date.parse(t.sla_due_at) : null
    const overdue = due !== null && due < Date.now()
    if (overdue) out.sla++
    else if (due !== null && due <= endOfDay.getTime()) out.expiring++
    if (!out.first || overdue)
      out.first = { id: t.id, due: t.sla_due_at, unassigned: !t.assignee_name, overdue }
  }
  return out
}

function factsOf(n: Newcomer): DashboardItem {
  const extra = EXTRA[n.id] ?? {}
  const tk = ticketsOf(n.id)
  const ip = addDays(n.start_date, 90)
  const today = localIsoDate()
  const silent = contacted.has(n.id) ? null : silentDays(n)
  return {
    newcomer_id: n.id,
    zone: n.risk === 'red' ? 'red' : n.risk === 'yellow' ? 'yellow' : null,
    silent_days: silent,
    catchup: extra.catchup ?? null,
    ip_decision_date: ip >= today && ip <= addDays(today, 7) ? ip : null,
    tickets_open: tk.open,
    tickets_sla_overdue: tk.sla,
  }
}

function active(site?: string): Newcomer[] {
  return [...newcomers.values()]
    .map(view)
    .filter((n) => n.stage !== 'done')
    .filter((n) => !site || n.site === site)
}

function mineFor(req: MockRequest): Newcomer[] {
  const me = currentUser()
  return active(req.query.site).filter((n) => !me || n.recruiter_id === me.id)
}

function row(
  n: Newcomer,
  kind: string,
  zone: Zone,
  reason: string,
  actions: TodayActionId[],
  extra: Partial<TodayRow> = {},
): TodayRow {
  return {
    id: `${kind}-${n.id}`,
    kind,
    zone,
    reason,
    newcomer_id: n.id,
    newcomer_name: n.full_name,
    ticket_id: null,
    due_at: null,
    day: n.day,
    actions,
    ...extra,
  }
}

function tile(value: number, red: boolean, ids?: string[], secondary?: number): DashTile {
  return { value, red, ids, secondary }
}

function recruiterDashboard(req: MockRequest): MockResult {
  const list = mineFor(req)
  const items = list.map(factsOf)
  const byId = new Map(items.map((i) => [i.newcomer_id, i]))
  const [from, to] = weekBounds()
  const today = localIsoDate()
  const rows: TodayRow[] = []
  for (const n of list) {
    const f = byId.get(n.id)!
    const tk = ticketsOf(n.id)
    if (n.connection === 'invited' && n.day >= -1)
      rows.push(
        row(
          n,
          'invite_not_opened',
          'red',
          n.day < 0 ? 'выход завтра, приглашение не открыто' : 'вышел, приглашение не открыто',
          ['contact', 'open'],
        ),
      )
    if (tk.first && (tk.first.overdue || tk.expiring > 0))
      rows.push(
        row(
          n,
          'sla',
          tk.first.overdue ? 'red' : 'yellow',
          tk.first.overdue ? 'SLA обращения просрочен' : 'SLA обращения истекает сегодня',
          tk.first.unassigned ? ['take', 'open'] : ['open'],
          { ticket_id: tk.first.id, due_at: tk.first.due },
        ),
      )
    if (f.silent_days !== null)
      rows.push(
        row(
          n,
          'silent',
          f.silent_days >= SILENT_RED ? 'red' : 'yellow',
          `не отвечает ${f.silent_days} дн. после 2 напоминаний`,
          ['contact', 'remind'],
        ),
      )
    if (n.day === 0)
      rows.push(row(n, 'no_plan', 'yellow', 'выход сегодня, плана от руководителя нет', ['open']))
    if (f.ip_decision_date && n.day >= 87)
      rows.push(row(n, 'ip_decision', 'yellow', 'решение по ИС через 3 дня, не принято', ['open']))
    if ((EXTRA[n.id]?.behind ?? 0) >= 2)
      rows.push(
        row(
          n,
          'catchup_behind',
          'yellow',
          `догоняет ${f.catchup?.done}/${f.catchup?.total}, отстаёт на ${EXTRA[n.id]?.behind} дня`,
          ['contact', 'open'],
        ),
      )
  }
  const ids = (pred: (i: DashboardItem, n: Newcomer) => boolean) =>
    items.filter((i, k) => pred(i, list[k])).map((i) => i.newcomer_id)
  const silent = ids((i) => i.silent_days !== null)
  const slaIds = ids((i) => i.tickets_open > 0)
  const slaOverdue = items.reduce((s, i) => s + i.tickets_sla_overdue, 0)
  const slaExpiring = list.reduce((s, n) => s + ticketsOf(n.id).expiring, 0)
  const startingWeek = ids((_, n) => n.start_date >= from && n.start_date <= to)
  const startingToday = list.filter((n) => n.start_date === today).length
  const ip = ids((i) => i.ip_decision_date !== null)
  const low = ids((_, n) => n.last_pulse !== null && n.last_pulse <= LOW_PULSE)
  const catching = ids((i) => i.catchup !== null)
  const answered = list.filter((n) => n.last_pulse !== null).length
  const data: RecruiterDashboard = {
    today: rows,
    tiles: {
      not_answering: tile(silent.length, silent.length >= 3, silent),
      sla: tile(slaExpiring, slaOverdue > 0, slaIds, slaOverdue),
      starting: tile(
        startingToday,
        list.some((n) => n.day >= -1 && n.day <= 0 && n.connection === 'invited'),
        startingWeek,
        startingWeek.length,
      ),
      ip_decisions: tile(ip.length, false, ip),
      low_pulse: tile(low.length, answered > 0 && low.length / answered >= 0.2, low),
      catching_up: tile(
        catching.length,
        list.some((n) => (EXTRA[n.id]?.behind ?? 0) >= 2),
        catching,
      ),
    },
    bot_did: { nudges: botNudges, summaries: BOT_SUMMARIES },
    pulse_week: {
      sent: list.length,
      answered,
      low: low.length,
      tone: answered >= 5 ? { positive: 3, neutral: 4, negative: 1, n: 8 } : null,
    },
    items,
    next_event: list[0]
      ? {
          title: 'Опрос недели',
          at: new Date(Date.now() + 86_400_000).toISOString(),
          newcomer_name: list[0].full_name,
        }
      : null,
    newcomers_led: list.length,
  }
  return { status: 200, data }
}

function hrLeadDashboard(req: MockRequest): MockResult {
  if (!isLead()) return forbidden()
  const all = active(req.query.site)
  const rows: TodayRow[] = []
  let unassignedTickets = 0
  let overdue = 0
  for (const t of tickets.values()) {
    if (t.status === 'resolved') continue
    if (req.query.site && t.site !== req.query.site) continue
    const n = newcomers.get(t.newcomer_id)
    const due = t.sla_due_at ? Date.parse(t.sla_due_at) : null
    const isOverdue = due !== null && due < Date.now()
    if (isOverdue) overdue++
    const base = {
      newcomer_id: t.newcomer_id,
      newcomer_name: n?.full_name ?? t.newcomer_name,
      ticket_id: t.id,
      due_at: t.sla_due_at,
      day: null,
    }
    if (!t.assignee_name && Date.now() - Date.parse(t.created_at) > 2 * 3_600_000) {
      unassignedTickets++
      rows.push({
        id: `ua-${t.id}`,
        kind: 'unassigned_ticket',
        zone: 'red',
        reason: `обращение №${t.number} без исполнителя больше 2 ч`,
        actions: ['assign'],
        ...base,
      })
    } else if (isOverdue) {
      rows.push({
        id: `sla-${t.id}`,
        kind: 'sla_overdue',
        zone: 'red',
        reason: `SLA обращения №${t.number} просрочен`,
        recruiter_name: t.assignee_name,
        actions: ['reassign', 'open'],
        ...base,
        ticket_id: t.id,
      })
    }
  }
  // One aggregate row for all of them, as the server sends it.
  const noRecruiter = all.filter((n) => !n.recruiter_id)
  if (noRecruiter.length > 0)
    rows.push({
      id: 'no-recruiter',
      kind: 'no_recruiter',
      zone: 'yellow',
      reason: `Без рекрутера: ${noRecruiter.length}`,
      newcomer_id: null,
      newcomer_name: null,
      count: noRecruiter.length,
      ticket_id: null,
      due_at: null,
      day: null,
      actions: ['pin'],
    })
  rows.push({
    id: 'theme-F2',
    kind: 'theme_no_owner',
    zone: 'yellow',
    reason: 'тема недели «доступы и СИЗ» без ответственного',
    theme: 'Доступы и СИЗ',
    newcomer_id: null,
    newcomer_name: null,
    ticket_id: null,
    due_at: null,
    day: null,
    actions: ['owner'],
  })
  const load: LoadRow[] = [...users.values()]
    .filter((u) => u.role === 'recruiter' && u.active !== false)
    .map((u) => {
      const mine = all.filter((n) => n.recruiter_id === u.id)
      const tk = mine.map((n) => ticketsOf(n.id))
      return {
        user_id: u.id,
        full_name: u.full_name,
        newcomers: mine.length,
        tickets_open: tk.reduce((s, x) => s + x.open, 0),
        tickets_sla_overdue: tk.reduce((s, x) => s + x.sla, 0),
      }
    })
  const answered = all.filter((n) => n.last_pulse !== null).length
  const today = localIsoDate()
  const [, weekEnd] = weekBounds()
  const data: HrLeadDashboard = {
    today: rows,
    tiles: {
      unassigned_tickets: tile(unassignedTickets, unassignedTickets > 0),
      sla_overdue: tile(overdue, overdue > 0),
      no_recruiter: tile(noRecruiter.length, false),
      red_zone: tile(all.filter((n) => n.risk === 'red').length, false),
      pulse_response: tile(answered, false, undefined, all.length),
      ip_week: tile(
        all.filter((n) => {
          const ip = addDays(n.start_date, 90)
          return ip >= today && ip <= weekEnd
        }).length,
        false,
      ),
    },
    load,
    median_first_response_min: 47,
    digest: {
      week: '2026-W43',
      lines: [
        'Индекс W 81 (▲ 4), ответили 55 из 67',
        'Главная тема: доступы и СИЗ — 11 новичков, GrPP и GTP',
        'GTP: W 75 (▼ 3) · GrPP: W 84 (▲ 6) · Luzhniki: мало данных',
      ],
    },
    bot_did: { nudges: botNudges + 12, summaries: BOT_SUMMARIES + 3 },
  }
  return { status: 200, data }
}

// Reminder history per newcomer (api_contract.md, "Nudges").
const nudges = new Map<string, NudgeItem[]>()
let nudgesSeeded = false
export function seedNudges(): void {
  if (nudgesSeeded) return
  nudgesSeeded = true
  for (const [id, x] of Object.entries(EXTRA)) {
    const n = newcomers.get(id)
    if (n && x.catchup) n.catchup = x.catchup
  }
  for (const n of newcomers.values()) {
    if (!n.unresponsive_since) continue
    const base = Date.parse(n.unresponsive_since)
    nudges.set(n.id, [
      nudge(n.id, 'auto', 1, new Date(base - 20 * 3_600_000).toISOString()),
      nudge(n.id, 'auto', 2, new Date(base).toISOString()),
    ])
  }
}

function openQuestion(id: string): { event_id: string; title: string } | null {
  // A sent step still waiting for an answer: a survey first, else any sent step.
  const sent = (timelines.get(id) ?? []).filter((e) => e.status === 'sent' && !e.answer)
  const ev = sent.find((e) => e.kind === 'survey') ?? sent[sent.length - 1]
  return ev ? { event_id: ev.event_id, title: ev.title } : null
}

function nudge(id: string, kind: NudgeItem['kind'], n: number, at: string): NudgeItem {
  const q = openQuestion(id)
  return {
    id: crypto.randomUUID(),
    event_id: kind === 'contacted' ? null : (q?.event_id ?? null),
    event_title: kind === 'contacted' ? null : (q?.title ?? null),
    kind,
    n,
    at,
    planned: false,
    delivered: true,
    created_by: kind === 'auto' ? null : (currentUser()?.id ?? null),
  }
}

function markContacted(req: MockRequest): MockResult {
  if (!newcomers.has(req.params.id)) return err(404, 'NOT_FOUND', 'Новичок не найден')
  contacted.add(req.params.id)
  const list = nudges.get(req.params.id) ?? []
  list.push(nudge(req.params.id, 'contacted', 0, new Date().toISOString()))
  nudges.set(req.params.id, list)
  return { status: 204 }
}

function listNudges(req: MockRequest): MockResult {
  const n = newcomers.get(req.params.id)
  if (!n) return err(404, 'NOT_FOUND', 'Новичок не найден')
  const items = (nudges.get(n.id) ?? []).slice().sort((a, b) => Date.parse(a.at) - Date.parse(b.at))
  return { status: 200, data: { items, total: items.length, tz: zoneOf(n) } }
}

const IP_VALUES: IpDecision[] = ['keep', 'extend', 'part']

function ipDecision(req: MockRequest): MockResult {
  const n = newcomers.get(req.params.id)
  if (!n) return err(404, 'NOT_FOUND', 'Новичок не найден')
  const me = currentUser()
  if (me && me.role === 'manager') return forbidden()
  const d = req.body.decision
  if (d !== null && !IP_VALUES.includes(d as IpDecision))
    return err(422, 'VALIDATION_FAILED', 'Решение: оставить, продлить или расстаться')
  n.ip_decision = (d as IpDecision | null) ?? null
  n.ip_decision_at = d ? new Date().toISOString() : null
  n.ip_decision_by = d ? (me?.full_name ?? 'HR') : null
  return { status: 200, data: view(n) }
}

function devRecruiterMorning(): MockResult {
  if (!isLead()) return forbidden()
  const sent = [...users.values()].filter((u) => u.role === 'recruiter' && u.active !== false)
  return {
    status: 200,
    data: {
      sent: sent.length,
      items: sent.map((u) => ({
        user_id: u.id,
        text: 'Доброе утро! Сегодня: срочные обращения — 1, не отвечают после напоминаний — 2.',
      })),
    },
  }
}

// The multipart body does not reach the mock (parseBody keeps objects only),
// so the answer names a generic file.
function uploadFile(req: MockRequest): MockResult {
  if (!newcomers.has(req.params.id)) return err(404, 'NOT_FOUND', 'Новичок не найден')
  return {
    status: 201,
    data: {
      name: 'adaptation_plan',
      filename: 'adaptation_plan.pdf',
      mime: 'application/pdf',
      size: 120_000,
      updated_at: new Date().toISOString(),
    },
  }
}

// 200 sent; 202 planned when it is outside 8-18 of the newcomer's zone (a
// second press returns the same planned nudge); 409 nothing to remind about.
function remind(req: MockRequest): MockResult {
  const n = newcomers.get(req.params.id)
  if (!n) return err(404, 'NOT_FOUND', 'Новичок не найден')
  if (n.connection !== 'connected')
    return err(409, 'CONFLICT', 'Новичок не подключён к боту — напоминать некуда')
  const q = openQuestion(n.id)
  if (!q) return err(409, 'NOTHING_TO_REMIND', 'Напоминать не о чем: вопросов без ответа нет')
  const tz = zoneOf(n)
  const list = nudges.get(n.id) ?? []
  const pending = list.find((x) => x.planned)
  if (pending)
    return {
      status: 202,
      data: {
        status: 'planned',
        at: pending.at,
        tz,
        message: 'Отправится в 08:00 по времени новичка',
        nudge: pending,
      },
    }
  const hour = Number(
    new Intl.DateTimeFormat('en-GB', { timeZone: tz, hour: '2-digit', hourCycle: 'h23' }).format(
      new Date(),
    ),
  )
  const count = list.filter((x) => x.event_id === q.event_id).length + 1
  const item = nudge(n.id, 'manual', count, new Date().toISOString())
  list.push(item)
  nudges.set(n.id, list)
  if (hour < 8 || hour >= 18) {
    // 08:00 on the newcomer's clock: today before 8, tomorrow after 18.
    const now = new Date()
    const off = utcOffsetMinutes(tz, now)
    const local = new Date(now.getTime() + off * 60_000)
    if (hour >= 18) local.setUTCDate(local.getUTCDate() + 1)
    local.setUTCHours(8, 0, 0, 0)
    item.planned = true
    item.delivered = false
    item.at = new Date(local.getTime() - off * 60_000).toISOString()
    return {
      status: 202,
      data: {
        status: 'planned',
        at: item.at,
        tz,
        message: 'Отправится в 08:00 по времени новичка',
        nudge: item,
      },
    }
  }
  botNudges++
  return {
    status: 200,
    data: { status: 'sent', at: item.at, tz, message: 'Напоминание отправлено', nudge: item },
  }
}

function team(): MockResult {
  if (!isLead()) return forbidden()
  const all = [...newcomers.values()].map(view).filter((n) => n.stage !== 'done')
  const items: TeamMember[] = [...users.values()]
    .filter((u) => u.role === 'recruiter')
    .map((u) => {
      const mine = all.filter((n) => n.recruiter_id === u.id)
      let open = 0
      let sla = 0
      for (const n of mine) {
        const tk = ticketsOf(n.id)
        open += tk.open
        sla += tk.sla
      }
      return {
        user_id: u.id,
        full_name: u.full_name,
        login: u.login,
        active: u.active !== false,
        newcomers: mine.length,
        tickets_open: open,
        tickets_sla_overdue: sla,
      }
    })
  return {
    status: 200,
    data: { items, unassigned: all.filter((n) => !n.recruiter_id).length },
  }
}

function listUsers(req: MockRequest): MockResult {
  if (!isLead()) return forbidden()
  const items = [...users.values()]
    .filter((u) => !req.query.role || u.role === req.query.role)
    .filter((u) => req.query.active !== '1' || u.active !== false)
    .map(publicUser)
  return { status: 200, data: { items, total: items.length } }
}

const ROLES: Role[] = ['hr_lead', 'recruiter', 'manager']

function createUser(req: MockRequest): MockResult {
  if (!isLead()) return forbidden()
  const b = req.body
  const login = typeof b.login === 'string' ? b.login.trim() : ''
  const full_name = typeof b.full_name === 'string' ? b.full_name.trim() : ''
  const password = typeof b.password === 'string' ? b.password : ''
  const role = b.role as Role
  if (!login || !full_name || password.length < 8 || !ROLES.includes(role))
    return err(422, 'VALIDATION_FAILED', 'Проверьте логин, имя, пароль (от 8 символов) и роль')
  if ([...users.values()].some((u) => u.login === login))
    return err(409, 'LOGIN_TAKEN', 'Такой логин уже есть')
  const id = crypto.randomUUID()
  const u = {
    id,
    login,
    full_name,
    role,
    tz: null,
    manager_name: typeof b.manager_name === 'string' ? b.manager_name : null,
    active: true,
    password,
  }
  users.set(id, u)
  return { status: 201, data: publicUser(u) }
}

function patchUser(req: MockRequest): MockResult {
  if (!isLead()) return forbidden()
  const u = users.get(req.params.id)
  if (!u) return err(404, 'NOT_FOUND', 'Пользователь не найден')
  if (typeof req.body.active === 'boolean') {
    if (!req.body.active && currentUser()?.id === u.id)
      return err(409, 'SELF_DEACTIVATE', 'Нельзя деактивировать самого себя')
    u.active = req.body.active
  }
  if (ROLES.includes(req.body.role as Role)) u.role = req.body.role as Role
  if (typeof req.body.full_name === 'string' && req.body.full_name.trim())
    u.full_name = req.body.full_name.trim()
  return { status: 200, data: publicUser(u) }
}

function reassign(req: MockRequest): MockResult {
  if (!isLead()) return forbidden()
  const ids = Array.isArray(req.body.newcomer_ids) ? (req.body.newcomer_ids as string[]) : []
  const target = req.body.recruiter_id
  if (target !== null && (typeof target !== 'string' || !users.get(target)?.active))
    return err(422, 'VALIDATION_FAILED', 'Рекрутер не найден или деактивирован')
  let updated = 0
  for (const id of ids) {
    const n = newcomers.get(id)
    if (!n) continue
    n.recruiter_id = target
    n.recruiter_name = target ? (users.get(target)?.full_name ?? '') : ''
    updated++
  }
  return { status: 200, data: { updated } }
}

const routes: MockRoute[] = [
  { method: 'GET', path: '/recruiter/dashboard', handler: recruiterDashboard },
  { method: 'GET', path: '/hrlead/dashboard', handler: hrLeadDashboard },
  { method: 'POST', path: '/newcomers/:id/contacted', handler: markContacted },
  { method: 'POST', path: '/newcomers/:id/remind', handler: remind },
  { method: 'POST', path: '/newcomers/:id/nudge', handler: remind },
  { method: 'GET', path: '/newcomers/:id/nudges', handler: listNudges },
  { method: 'POST', path: '/newcomers/:id/ip-decision', handler: ipDecision },
  { method: 'POST', path: '/dev/recruiter-morning', handler: devRecruiterMorning },
  { method: 'POST', path: '/newcomers/:id/files', handler: uploadFile },
  { method: 'GET', path: '/team', handler: team },
  { method: 'GET', path: '/users', handler: listUsers },
  { method: 'POST', path: '/users', handler: createUser },
  { method: 'PATCH', path: '/users/:id', handler: patchUser },
  { method: 'POST', path: '/newcomers/reassign', handler: reassign },
]

export const teamRoutes: MockRoute[] = routes.map((r) => ({
  ...r,
  handler: (req) => {
    ensureSeeded()
    seedNudges()
    return r.handler(req)
  },
}))
