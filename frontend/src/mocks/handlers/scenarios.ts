import type { MockRequest, MockResult, MockRoute } from '../adapter'
import type { ScenarioDetail, ScenarioStep } from '@/types/api'
import { importedScenario } from '../fixtures/scenarios'
import { activeCount, ensureSeeded, newId, scenarios } from '../state'

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
  s.updated_at = new Date().toISOString()
  return { status: 200, data: detail(s) }
}

function fileError(message: string): MockResult {
  return error(422, 'VALIDATION_FAILED', message, [
    { field: 'file', code: 'INVALID_FORMAT', message },
  ])
}

function importExcel(req: MockRequest): MockResult {
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
  const name = form instanceof FormData ? String(form.get('name') ?? '').trim() : ''
  const s = importedScenario(newId(), name || file.name, new Date().toISOString())
  scenarios.set(s.id, s)
  return { status: 201, data: detail(s) }
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
  { method: 'PUT', path: '/scenarios/:id/steps', handler: putSteps },
  { method: 'POST', path: '/scenarios/:id/publish', handler: publish },
  { method: 'POST', path: '/scenarios/import', handler: importExcel },
]

export const scenarioRoutes: MockRoute[] = routes.map((r) => ({
  ...r,
  handler: (req) => {
    ensureSeeded()
    return r.handler(req)
  },
}))
