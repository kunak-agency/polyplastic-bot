import type { MockRequest, MockResult, MockRoute } from '../adapter'
import type {
  PublishPreview,
  ScenarioDetail,
  ScenarioStep,
  SiteCode,
  StepChange,
} from '@/types/api'
import { importedScenario } from '../fixtures/scenarios'
import { SCENARIO_BRANCHES_ID, syntheticStats } from '../fixtures/scenarioBranches'
import { activeCount, ensureSeeded, newcomers, newId, scenarios } from '../state'

// Mirrors backend/handlers/scenarios.go and in/scenario.go: 422
// VALIDATION_FAILED with details[] naming steps[N].field, 404 NOT_FOUND.

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

function notFound(): MockResult {
  return error(404, 'NOT_FOUND', 'Сценарий не найден')
}

// A copy with the live counter, so a caller's edits never touch the store.
function detail(s: ScenarioDetail): ScenarioDetail {
  return structuredClone({ ...s, active_newcomers: activeCount(s.id) })
}

const KINDS = ['text', 'card', 'file', 'survey', 'task']
const SCALES = ['1-10', '1-5', 'yesno', 'choice']

// The checks of in.ValidateSteps that a screen can trip over.
function validateSteps(steps: ScenarioStep[]): FieldError[] {
  const out: FieldError[] = []
  steps.forEach((s, i) => {
    const p = `steps[${i}].`
    if (typeof s.day_offset !== 'number')
      out.push({ field: p + 'day_offset', code: 'REQUIRED', message: 'Обязательное поле' })
    if (!String(s.title ?? '').trim())
      out.push({ field: p + 'title', code: 'REQUIRED', message: 'Обязательное поле' })
    if (s.time && !/^([01]\d|2[0-3]):[0-5]\d$/.test(s.time))
      out.push({ field: p + 'time', code: 'INVALID_FORMAT', message: 'Время в формате HH:MM' })
    if (s.kind && !KINDS.includes(s.kind))
      out.push({ field: p + 'kind', code: 'OUT_OF_RANGE', message: 'Тип: ' + KINDS.join(', ') })
    if (!s.survey) {
      if (s.kind === 'survey')
        out.push({
          field: p + 'survey',
          code: 'REQUIRED',
          message: 'У опроса нужны шкала и вопросы',
        })
      return
    }
    if (!SCALES.includes(s.survey.scale))
      out.push({
        field: p + 'survey.scale',
        code: 'OUT_OF_RANGE',
        message: 'Шкала: ' + SCALES.join(', '),
      })
    const qs = s.survey.questions ?? []
    if (qs.length === 0 || qs.length > 20)
      out.push({
        field: p + 'survey.questions',
        code: 'OUT_OF_RANGE',
        message: 'От 1 до 20 вопросов',
      })
    qs.forEach((q, j) => {
      if (!String(q.text ?? '').trim())
        out.push({
          field: `${p}survey.questions[${j}].text`,
          code: 'REQUIRED',
          message: 'Обязательное поле',
        })
      if (s.survey?.scale === 'choice' && (q.options ?? []).length < 2)
        out.push({
          field: `${p}survey.questions[${j}].options`,
          code: 'OUT_OF_RANGE',
          message: 'У выбора нужны хотя бы два варианта',
        })
    })
  })
  return out
}

// Steps of the last published version, per scenario: a copy starts from them
// (services.CreateDraft). Seeded scenarios have no snapshot here; their draft
// equals the published version, so the draft is copied instead.
const publishedSteps = new Map<string, ScenarioStep[]>()

const SITE_CODES: SiteCode[] = ['GrPP', 'Luzhniki', 'GTP']

function createScenario(req: MockRequest): MockResult {
  const name = String(req.body.name ?? '').trim()
  if (!name)
    return error(422, 'VALIDATION_FAILED', 'Проверьте данные запроса', [
      { field: 'name', code: 'REQUIRED', message: 'Обязательное поле' },
    ])
  const rawSites = Array.isArray(req.body.sites) ? (req.body.sites as string[]) : []
  if (rawSites.some((c) => !SITE_CODES.includes(c as SiteCode)))
    return error(422, 'VALIDATION_FAILED', 'Проверьте данные запроса', [
      { field: 'sites', code: 'OUT_OF_RANGE', message: 'Площадки: GrPP, Luzhniki, GTP' },
    ])
  const folded = name.toLowerCase()
  for (const s of scenarios.values())
    if (s.name.trim().toLowerCase() === folded)
      return {
        status: 409,
        data: {
          error: {
            code: 'scenario_name_taken',
            message: 'Сценарий с таким названием уже есть — выберите другое',
            traceId: `mock-${Date.now()}`,
          },
        },
      }
  let steps: ScenarioStep[] = []
  const fromId = req.body.from_scenario_id
  if (typeof fromId === 'string' && fromId) {
    const src = scenarios.get(fromId)
    if (!src)
      return error(422, 'VALIDATION_FAILED', 'Сценарий-источник не найден', [
        {
          field: 'from_scenario_id',
          code: 'INVALID_FORMAT',
          message: 'Сценарий-источник не найден',
        },
      ])
    // New ids: the copy's steps must not be mistaken for the source's in timelines.
    steps = structuredClone(publishedSteps.get(src.id) ?? src.steps).map((st) => ({
      ...st,
      id: newId(),
    }))
  }
  const s: ScenarioDetail = {
    id: newId(),
    name,
    sites: [...new Set(rawSites as SiteCode[])],
    version: 0,
    status: 'draft',
    active_newcomers: 0,
    updated_at: new Date().toISOString(),
    steps,
  }
  scenarios.set(s.id, s)
  return { status: 201, data: detail(s) }
}

function deleteScenario(req: MockRequest): MockResult {
  const s = scenarios.get(req.params.id)
  if (!s) return notFound()
  const hasNewcomers = [...newcomers.values()].some((n) => n.scenario_id === s.id)
  if (s.version > 0 || hasNewcomers)
    return {
      status: 409,
      data: {
        error: {
          code: 'scenario_not_deletable',
          message: hasNewcomers
            ? 'По сценарию запущены новички — удалить его нельзя'
            : 'Сценарий уже публиковался — удалить можно только черновик без публикаций',
          traceId: `mock-${Date.now()}`,
        },
      },
    }
  scenarios.delete(s.id)
  publishedSteps.delete(s.id)
  return { status: 204 }
}

function putSteps(req: MockRequest): MockResult {
  const s = scenarios.get(req.params.id)
  if (!s) return notFound()
  // { steps } per contract; a bare array is accepted too, as on the server.
  const raw: unknown = Array.isArray(req.body) ? req.body : req.body.steps
  if (!Array.isArray(raw))
    return error(422, 'VALIDATION_FAILED', 'Тело запроса — не JSON нужной формы')
  const steps = structuredClone(raw as ScenarioStep[])
  const problems = validateSteps(steps)
  if (problems.length > 0)
    return error(422, 'VALIDATION_FAILED', 'Проверьте данные запроса', problems)
  // The server keeps a client id and makes its own when there is none; an
  // empty time comes back as 10:00, the absent flag as "work hours only".
  s.steps = steps.map((st) => ({
    ...st,
    id: st.id || newId(),
    time: st.time || '10:00',
    sites: st.sites ?? [],
    buttons: st.buttons ?? [],
    work_hours_only: st.work_hours_only ?? true,
  }))
  s.status = 'draft'
  s.updated_at = new Date().toISOString()
  return { status: 200, data: detail(s) }
}

function publish(req: MockRequest): MockResult {
  const s = scenarios.get(req.params.id)
  if (!s) return notFound()
  // The comment is optional on the server (the dialog asks for it anyway);
  // apply defaults to "new".
  const apply = req.body.apply ?? 'new'
  if (apply !== 'new' && apply !== 'active')
    return error(422, 'VALIDATION_FAILED', 'Проверьте данные запроса', [
      { field: 'apply', code: 'OUT_OF_RANGE', message: 'apply: new или active' },
    ])
  if (s.steps.length === 0)
    return error(422, 'VALIDATION_FAILED', 'В сценарии нет шагов — публиковать нечего')
  s.version += 1
  s.status = 'published'
  publishedSteps.set(s.id, structuredClone(s.steps))
  s.updated_at = new Date().toISOString()
  return { status: 200, data: detail(s) }
}

function fileError(message: string): MockResult {
  return error(422, 'VALIDATION_FAILED', message, [
    { field: 'file', code: 'INVALID_FORMAT', message },
  ])
}

// The Excel file of a multipart body, or the server's refusal.
function excelFile(req: MockRequest): File | MockResult {
  // The adapter hands a FormData body through untouched.
  const form = req.body as unknown
  const file = form instanceof FormData ? form.get('file') : null
  if (!(file instanceof File))
    return error(422, 'VALIDATION_FAILED', 'Нужен файл Excel в поле file', [
      { field: 'file', code: 'REQUIRED', message: 'Обязательное поле' },
    ])
  // excelize reads OOXML only: the server refuses Excel 97-2003 with a hint.
  if (/\.xls$/i.test(file.name))
    return fileError('Формат .xls (Excel 97–2003) пока не читается — сохраните файл как .xlsx')
  if (!/\.xlsx$/i.test(file.name)) return fileError('Нужен файл Excel: .xlsx')
  return file
}

// POST /scenarios/:id/import: the file's steps replace the draft.
function importInto(req: MockRequest): MockResult {
  const s = scenarios.get(req.params.id)
  if (!s) return notFound()
  const file = excelFile(req)
  if (!(file instanceof File)) return file
  s.steps = importedScenario(s.id, file.name, new Date().toISOString()).steps
  s.status = 'draft'
  s.updated_at = new Date().toISOString()
  return { status: 200, data: { ...detail(s), import_report: mockImportReport(s.steps) } }
}

// import_report of the mock import (api_contract.md v4): counts from the fixture
// steps and a few warnings that point at real steps, so the report screen and
// "open the step" work without the backend.
function mockImportReport(steps: ScenarioDetail['steps']) {
  const blocks = new Set(steps.map((x) => `${x.day_offset}|${x.time}`))
  const media = new Set(steps.flatMap((x) => (x.media ?? []).map((m) => m.name)))
  const pick = (i: number) => steps[Math.min(i, Math.max(steps.length - 1, 0))]
  const warn = (row: number, code: string, message: string, i: number) => {
    const st = pick(i)
    return { row, code, message, step_key: st?.key ?? '', step_id: st?.id ?? '' }
  }
  return {
    blocks: blocks.size,
    messages: steps.length,
    questions: steps.filter((x) => x.buttons.length > 0 || !!x.survey).length,
    branches: steps.reduce((n, x) => n + x.buttons.filter((b) => !!b.branch).length, 0),
    rules: 2,
    media_names: media.size,
    media_name_list: [...media],
    warnings: steps.length
      ? [
          warn(3, 'no_text', 'Текст-заглушка («…») — допишите сообщение', 0),
          warn(
            8,
            'alt_text',
            'У текста два авторских варианта: первый взят в сообщение, второй — в альтернативу',
            1,
          ),
          warn(
            19,
            'hr_task',
            'Письмо HR кандидату: бот его не отправляет — шаг стал задачей HR',
            2,
          ),
          warn(
            34,
            'recruiter_flag',
            'Шаг «по отмашке рекрутера»: уйдёт, только если у новичка включён флаг recruiter_enabled',
            3,
          ),
          warn(
            45,
            'photo_unnamed',
            'Вложение «ФОТО» без имени файла — загрузите фото в медиатеку',
            4,
          ),
          warn(
            57,
            'survey_outcomes',
            'Реакции на ответы опроса добавлены к опросам блока — проверьте условия',
            5,
          ),
        ]
      : [],
  }
}

function importExcel(req: MockRequest): MockResult {
  const file = excelFile(req)
  if (!(file instanceof File)) return file
  const form = req.body as unknown
  const name = form instanceof FormData ? String(form.get('name') ?? '').trim() : ''
  const s = importedScenario(newId(), name || file.name, new Date().toISOString())
  scenarios.set(s.id, s)
  return { status: 201, data: { ...detail(s), import_report: mockImportReport(s.steps) } }
}

// ── v4: stats, publish preview, send-test (api_contract.md v4) ─────────────

const MIN_ANSWERS = 5

function stats(req: MockRequest): MockResult {
  const s = scenarios.get(req.params.id)
  if (!s) return notFound()
  const site = req.query.site || null
  if (site && !SITE_CODES.includes(site as SiteCode))
    return error(422, 'VALIDATION_FAILED', 'Площадка: GrPP, Luzhniki или GTP', [
      { field: 'site', code: 'OUT_OF_RANGE', message: 'GrPP, Luzhniki или GTP' },
    ])
  let items = syntheticStats(s.steps, MIN_ANSWERS)
  // One site sees about a third of the traffic: some shares fall under the threshold.
  if (site)
    items = items.map((it) => {
      const answered = Math.floor(it.answered / 3)
      const buttons = it.buttons.map((b) => {
        const picked = Math.floor(b.picked / 3)
        return { ...b, picked, share: answered >= MIN_ANSWERS ? picked / answered : null }
      })
      const sent = Math.floor(it.sent / 3)
      return { ...it, sent, answered, no_answer: sent - answered, buttons }
    })
  return { status: 200, data: { site, min_answers: MIN_ANSWERS, items } }
}

// What the branches fixture was published as: one step differs and one more
// button with a branch existed, so the preview has something to show.
function seededPublished(s: ScenarioDetail): ScenarioStep[] | undefined {
  if (s.id !== SCENARIO_BRANCHES_ID) return undefined
  return structuredClone(s.steps).map((st) =>
    st.title === 'Пульс 3-го дня'
      ? {
          ...st,
          text_vy: 'Как первые дни?',
          buttons: [
            ...st.buttons,
            {
              label: 'Позже',
              action: '',
              branch: { messages: [], actions: [], then: 'end_block' as const },
            },
          ],
        }
      : st,
  )
}

function changedFields(a: ScenarioStep, b: ScenarioStep): StepChange['fields'] {
  const j = (x: unknown) => JSON.stringify(x ?? null)
  const plain = (l: ScenarioStep['buttons']) => l.map((x) => ({ ...x, branch: null }))
  const f: StepChange['fields'] = []
  if (a.day_offset !== b.day_offset || a.time !== b.time) f.push('when')
  if (a.text_ty !== b.text_ty || a.text_vy !== b.text_vy || a.title !== b.title) f.push('text')
  if (j(a.sites) !== j(b.sites)) f.push('sites')
  if (j(plain(a.buttons)) !== j(plain(b.buttons))) f.push('buttons')
  if (j(a.buttons.map((x) => x.branch)) !== j(b.buttons.map((x) => x.branch))) f.push('branches')
  if (j(a.media) !== j(b.media)) f.push('media')
  if (j(a.survey) !== j(b.survey)) f.push('survey')
  if (
    a.kind !== b.kind ||
    !!a.required !== !!b.required ||
    !!a.hr_task !== !!b.hr_task ||
    j(a.no_answer) !== j(b.no_answer) ||
    j(a.advance) !== j(b.advance)
  )
    f.push('settings')
  return f
}

function preview(req: MockRequest): MockResult {
  const s = scenarios.get(req.params.id)
  if (!s) return notFound()
  const published = publishedSteps.get(s.id) ?? seededPublished(s) ?? (s.version > 0 ? s.steps : [])
  const old = new Map(published.map((st) => [st.id, st]))
  const removedLabels = (prev: ScenarioStep, cur?: ScenarioStep) =>
    prev.buttons.map((b) => b.label).filter((l) => !cur?.buttons.some((x) => x.label === l))
  const changes: StepChange[] = []
  for (const st of s.steps) {
    const prev = old.get(st.id)
    const base = { step_id: st.id, title: st.title, day_offset: st.day_offset, time: st.time }
    if (!prev) {
      changes.push({ ...base, change: 'added', fields: [], buttons_removed: [] })
      continue
    }
    const fields = changedFields(prev, st)
    if (fields.length > 0)
      changes.push({ ...base, change: 'changed', fields, buttons_removed: removedLabels(prev, st) })
  }
  for (const st of published)
    if (!s.steps.some((x) => x.id === st.id))
      changes.push({
        step_id: st.id,
        title: st.title,
        day_offset: st.day_offset,
        time: st.time,
        change: 'removed',
        fields: [],
        buttons_removed: removedLabels(st),
      })
  // Live branches: as if a third of the active newcomers still had each
  // removed branch's message unanswered.
  const active = activeCount(s.id) || (s.id === SCENARIO_BRANCHES_ID ? 9 : 0)
  let removedBranches = 0
  for (const prev of published) {
    const cur = s.steps.find((x) => x.id === prev.id)
    for (const b of prev.buttons)
      if (b.branch && !cur?.buttons.some((x) => x.label === b.label && x.branch)) removedBranches++
  }
  const body: PublishPreview = {
    next_version: s.version + 1,
    active_newcomers: active,
    inside_survey: Math.floor(active / 4),
    removed_branches_live: removedBranches * Math.ceil(active / 3),
    recommend: changes.some((c) => c.buttons_removed.length > 0) ? 'new' : 'active',
    changes,
  }
  return { status: 200, data: body }
}

// PATCH /scenarios/:id {commands?, rules?} — the scenario's commands and rules.
function patchScenario(req: MockRequest): MockResult {
  const s = scenarios.get(req.params.id)
  if (!s) return notFound()
  const body = req.body as { commands?: unknown; rules?: unknown }
  if (body.commands !== undefined) {
    if (!Array.isArray(body.commands))
      return error(422, 'VALIDATION_FAILED', 'Проверьте данные запроса', [
        { field: 'commands', code: 'INVALID_FORMAT', message: 'Нужен список команд' },
      ])
    const bad = (body.commands as { name?: string }[]).findIndex(
      (c) => !/^\/[a-z0-9_]{1,32}$/i.test(String(c.name ?? '').trim()),
    )
    if (bad >= 0)
      return error(422, 'VALIDATION_FAILED', 'Проверьте данные запроса', [
        { field: `commands[${bad}].name`, code: 'INVALID_FORMAT', message: 'Команда — /латиница' },
      ])
    s.commands = structuredClone(body.commands as ScenarioDetail['commands'])
  }
  if (body.rules !== undefined) s.rules = structuredClone(body.rules as ScenarioDetail['rules'])
  s.updated_at = new Date().toISOString()
  return { status: 200, data: detail(s) }
}

function sendTest(req: MockRequest): MockResult {
  const s = scenarios.get(req.params.id)
  if (!s) return notFound()
  if (!s.steps.some((st) => st.id === req.params.stepId))
    return error(404, 'NOT_FOUND', 'Шаг не найден в черновике сценария')
  const nc = newcomers.get(String(req.body.newcomer_id ?? ''))
  if (!nc) return error(404, 'NOT_FOUND', 'Новичок не найден')
  if (nc.connection !== 'connected')
    return error(409, 'CONFLICT', 'Новичок не подключён к боту — отправлять некуда')
  return { status: 200, data: { status: 'sent', messages: 1 } }
}

const routes: MockRoute[] = [
  {
    method: 'GET',
    path: '/scenarios',
    handler: () => {
      const items = [...scenarios.values()].map((s) => {
        const { steps: _steps, ...rest } = detail(s)
        void _steps
        return rest
      })
      return { status: 200, data: { items, total: items.length } }
    },
  },
  {
    method: 'GET',
    path: '/scenarios/:id',
    handler: (req) => {
      const s = scenarios.get(req.params.id)
      return s ? { status: 200, data: detail(s) } : notFound()
    },
  },
  { method: 'POST', path: '/scenarios', handler: createScenario },
  { method: 'DELETE', path: '/scenarios/:id', handler: deleteScenario },
  { method: 'PATCH', path: '/scenarios/:id', handler: patchScenario },
  { method: 'PUT', path: '/scenarios/:id/steps', handler: putSteps },
  { method: 'POST', path: '/scenarios/:id/import', handler: importInto },
  { method: 'POST', path: '/scenarios/:id/publish', handler: publish },
  { method: 'POST', path: '/scenarios/:id/publish/preview', handler: preview },
  { method: 'GET', path: '/scenarios/:id/stats', handler: stats },
  { method: 'POST', path: '/scenarios/:id/steps/:stepId/send-test', handler: sendTest },
  { method: 'POST', path: '/scenarios/import', handler: importExcel },
]

export const scenarioRoutes: MockRoute[] = routes.map((r) => ({
  ...r,
  handler: (req) => {
    ensureSeeded()
    return r.handler(req)
  },
}))
