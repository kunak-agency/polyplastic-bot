import type { MockRequest, MockResult, MockRoute } from '../adapter'
import type { Ticket, TicketDetail, TicketMessage } from '@/types/api'
import { ensureSeeded, tickets } from '../state'

// Mirrors api_contract.md v2 (tickets). Where the contract is silent the mock
// picks the strict reading and the screens do not depend on it:
// - reply, note and resolve answer the ticket with messages (the cabinet
//   re-reads the ticket anyway);
// - F5/F9 come with restricted: true and quote: null in the list AND in the
//   card; the newcomer's messages come with text: null and hidden: true — only
//   the HR partner and security may read them, and there are no roles in v2.
//   F0/F4/F10 are hidden from the manager only: HR reads them (24.10);
// - PATCH notify_manager on a manager-hidden category → 422;
// - assign / reply on a resolved ticket → 409 CONFLICT.

const RESTRICTED = new Set(['F5', 'F9'])
const MANAGER_HIDDEN = new Set(['F0', 'F4', 'F5', 'F9', 'F10'])
const STATUSES = new Set(['open', 'assigned', 'human_active', 'resolved'])
const RESOLVED_TEXT = '✅ Вопрос закрыт. Вернулся 🐾 — если что, спрашивайте.'

function error(status: number, code: string, message: string, field?: string): MockResult {
  const details = field ? [{ field, code: 'REQUIRED', message: 'Обязательное поле' }] : undefined
  return { status, data: { error: { code, message, traceId: `mock-${Date.now()}`, details } } }
}

function str(v: unknown): string {
  return typeof v === 'string' ? v.trim() : ''
}

function notFound(): MockResult {
  return error(404, 'NOT_FOUND', 'Обращение не найдено')
}

function closed(): MockResult {
  return error(409, 'CONFLICT', 'Обращение уже решено')
}

function restricted(t: Ticket): boolean {
  return RESTRICTED.has(t.category)
}

function listItem(t: TicketDetail): Ticket {
  const { messages: _messages, ...rest } = t
  void _messages
  return restricted(t) ? { ...rest, quote: null, restricted: true } : { ...rest, restricted: false }
}

function detail(t: TicketDetail): TicketDetail {
  const hide = restricted(t)
  const messages = t.messages.map((m) =>
    hide && m.from === 'newcomer' ? { ...m, text: null, hidden: true } : { ...m },
  )
  return { ...listItem(t), messages }
}

function push(t: TicketDetail, m: Omit<TicketMessage, 'at' | 'hidden'>): void {
  const at = new Date().toISOString()
  t.messages.push({ ...m, at, hidden: false })
  t.updated_at = at
}

function list(req: MockRequest): MockResult {
  const { status, site, route, newcomer_id } = req.query
  if (status && !STATUSES.has(status))
    return error(422, 'VALIDATION_FAILED', 'Статус: open, assigned, human_active или resolved')
  // `mine` needs auth, which v2 does not have: accepted and ignored.
  const items = [...tickets.values()]
    .filter((t) => !status || t.status === status)
    .filter((t) => !site || t.site === site)
    .filter((t) => !route || t.route === route)
    .filter((t) => !newcomer_id || t.newcomer_id === newcomer_id)
    .sort((a, b) => b.number - a.number)
    .map(listItem)
  return { status: 200, data: { items, total: items.length } }
}

function get(req: MockRequest): MockResult {
  const t = tickets.get(req.params.id)
  return t ? { status: 200, data: detail(t) } : notFound()
}

function assign(req: MockRequest): MockResult {
  const t = tickets.get(req.params.id)
  if (!t) return notFound()
  if (t.status === 'resolved') return closed()
  const name = str(req.body.assignee_name)
  if (!name) return error(422, 'VALIDATION_FAILED', 'Укажите исполнителя', 'assignee_name')
  const comment = str(req.body.comment)
  const previous = t.assignee_name
  t.assignee_name = name
  t.status = 'assigned'
  t.updated_at = new Date().toISOString()
  if (comment || (previous && previous !== name))
    push(t, {
      from: 'staff',
      author: name,
      text: comment ? `Назначено: ${name}. ${comment}` : `Назначено: ${name}.`,
      internal: true,
    })
  return { status: 200, data: listItem(t) }
}

function reply(req: MockRequest): MockResult {
  const t = tickets.get(req.params.id)
  if (!t) return notFound()
  if (t.status === 'resolved') return closed()
  const text = str(req.body.text)
  if (!text) return error(422, 'VALIDATION_FAILED', 'Текст ответа пустой', 'text')
  push(t, { from: 'staff', author: t.assignee_name, text, internal: false })
  if (req.body.resolve === true) {
    t.status = 'resolved'
    t.sla_due_at = null
    push(t, { from: 'bot', author: null, text: RESOLVED_TEXT, internal: false })
  } else {
    t.status = 'human_active'
  }
  return { status: 200, data: detail(t) }
}

function note(req: MockRequest): MockResult {
  const t = tickets.get(req.params.id)
  if (!t) return notFound()
  const text = str(req.body.text)
  if (!text) return error(422, 'VALIDATION_FAILED', 'Текст заметки пустой', 'text')
  push(t, { from: 'staff', author: t.assignee_name, text, internal: true })
  return { status: 200, data: detail(t) }
}

function resolve(req: MockRequest): MockResult {
  const t = tickets.get(req.params.id)
  if (!t) return notFound()
  if (t.status !== 'resolved') {
    t.status = 'resolved'
    t.sla_due_at = null
    push(t, { from: 'bot', author: null, text: RESOLVED_TEXT, internal: false })
  }
  return { status: 200, data: detail(t) }
}

function patch(req: MockRequest): MockResult {
  const t = tickets.get(req.params.id)
  if (!t) return notFound()
  const notify = req.body.notify_manager
  if (typeof notify !== 'boolean')
    return error(422, 'VALIDATION_FAILED', 'Нужно notify_manager', 'notify_manager')
  if (notify && MANAGER_HIDDEN.has(t.category))
    return error(
      422,
      'VALIDATION_FAILED',
      'Эта категория руководителю не показывается — уведомить его нельзя',
    )
  t.notify_manager = notify
  t.updated_at = new Date().toISOString()
  return { status: 200, data: detail(t) }
}

const routes: MockRoute[] = [
  { method: 'PATCH', path: '/tickets/:id', handler: patch },
  { method: 'GET', path: '/tickets', handler: list },
  { method: 'GET', path: '/tickets/:id', handler: get },
  { method: 'POST', path: '/tickets/:id/assign', handler: assign },
  { method: 'POST', path: '/tickets/:id/reply', handler: reply },
  { method: 'POST', path: '/tickets/:id/note', handler: note },
  { method: 'POST', path: '/tickets/:id/resolve', handler: resolve },
]

export const ticketRoutes: MockRoute[] = routes.map((r) => ({
  ...r,
  handler: (req) => {
    ensureSeeded()
    return r.handler(req)
  },
}))
