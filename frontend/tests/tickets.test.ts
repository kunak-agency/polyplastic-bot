// Ticket queue rules (src/lib/tickets.ts): SLA timer, views, kanban order,
// visibility of F4/F5/F9/F10.
import { test } from 'node:test'
import assert from 'node:assert/strict'
import type { Ticket } from '../src/types/api.ts'
import {
  activeCount,
  addWorkingDays,
  applyView,
  isSecurity,
  isSecurityOverdue,
  viewsFor,
  canNotifyManager,
  signatureRole,
  durationParts,
  initials,
  isRestricted,
  kanbanColumns,
  slaState,
  sortTickets,
  ticketsOfNewcomer,
  viewCounts,
} from '../src/lib/tickets.ts'

const NOW = Date.parse('2026-11-04T12:00:00+03:00')
const MIN = 60_000

function at(minutesFromNow: number): string {
  return new Date(NOW + minutesFromNow * MIN).toISOString()
}

function ticket(n: number, extra: Partial<Ticket> = {}): Ticket {
  return {
    id: `t${n}`,
    number: n,
    newcomer_id: 'n1',
    newcomer_name: 'x',
    site: 'GTP',
    day: 0,
    category: 'F2',
    priority: 'P2',
    status: 'open',
    route: 'hr',
    assignee_name: null,
    anonymous: false,
    summary: 's',
    quote: 'q',
    sla_due_at: at(600),
    created_at: at(-60),
    updated_at: at(-60),
    notify_manager: false,
    ...extra,
  }
}

test('SLA: ok, last hour, overdue, none', () => {
  assert.deepEqual(slaState(ticket(1, { sla_due_at: at(192) }), NOW), { kind: 'ok', ms: 192 * MIN })
  assert.equal(slaState(ticket(1, { sla_due_at: at(60) }), NOW).kind, 'soon')
  assert.equal(slaState(ticket(1, { sla_due_at: at(61) }), NOW).kind, 'ok')
  const late = slaState(ticket(1, { sla_due_at: at(-65) }), NOW)
  assert.equal(late.kind, 'overdue')
  assert.equal(late.ms, -65 * MIN)
  assert.equal(slaState(ticket(1, { sla_due_at: null }), NOW).kind, 'none')
  assert.equal(slaState(ticket(1, { sla_due_at: 'garbage' }), NOW).kind, 'none')
  // A resolved ticket has no timer even with a deadline in the past.
  assert.equal(slaState(ticket(1, { status: 'resolved', sla_due_at: at(-5) }), NOW).kind, 'none')
})

test('SLA: zone offsets are compared as instants', () => {
  // 12:30 Moscow is 09:30 UTC: 30 minutes left, whatever the zone written.
  const msk = ticket(1, { sla_due_at: '2026-11-04T12:30:00+03:00' })
  const utc = ticket(1, { sla_due_at: '2026-11-04T09:30:00Z' })
  assert.equal(slaState(msk, NOW).ms, 30 * MIN)
  assert.equal(slaState(utc, NOW).ms, 30 * MIN)
})

test('duration parts round a started minute up and ignore the sign', () => {
  assert.deepEqual(durationParts(192 * MIN), { days: 0, hours: 3, minutes: 12 })
  assert.deepEqual(durationParts(-65 * MIN), { days: 0, hours: 1, minutes: 5 })
  assert.deepEqual(durationParts(30_000), { days: 0, hours: 0, minutes: 1 })
  assert.deepEqual(durationParts(0), { days: 0, hours: 0, minutes: 1 })
  assert.deepEqual(durationParts((2 * 1440 + 3 * 60 + 1) * MIN - 1), {
    days: 2,
    hours: 3,
    minutes: 1,
  })
})

test('views: unassigned, urgent, overdue, closed', () => {
  const list = [
    ticket(1),
    ticket(2, { assignee_name: 'A', status: 'assigned', priority: 'P1' }),
    ticket(3, { priority: 'P0', sla_due_at: at(-1) }),
    ticket(4, { status: 'resolved', priority: 'P0', sla_due_at: null }),
    ticket(5, { status: 'human_active', assignee_name: 'B', sla_due_at: at(-30) }),
  ]
  const ids = (v: Parameters<typeof applyView>[1]) => applyView(list, v, NOW).map((t) => t.number)
  assert.deepEqual(ids('all'), [1, 2, 3, 4, 5])
  assert.deepEqual(ids('unassigned'), [1, 3])
  // A resolved P0 is not urgent any more.
  assert.deepEqual(ids('urgent'), [2, 3])
  assert.deepEqual(ids('overdue'), [3, 5])
  assert.deepEqual(ids('closed'), [4])
  assert.deepEqual(viewCounts(list, NOW), {
    all: 5,
    unassigned: 2,
    urgent: 2,
    overdue: 2,
    closed: 1,
    security: 0,
  })
  assert.equal(activeCount(list), 4)
})

test('sorting: priority, then nearest deadline, then oldest; closed last', () => {
  const list = [
    ticket(1, { priority: 'P2', sla_due_at: at(10) }),
    ticket(2, { priority: 'P1', sla_due_at: at(300) }),
    ticket(3, { priority: 'P1', sla_due_at: at(-20) }),
    ticket(4, { priority: 'P0', sla_due_at: at(500) }),
    ticket(5, { priority: 'P1', sla_due_at: null, created_at: at(-500) }),
    ticket(6, { priority: 'P1', sla_due_at: null, created_at: at(-900) }),
    ticket(7, { status: 'resolved', priority: 'P0', updated_at: at(-100) }),
    ticket(8, { status: 'resolved', priority: 'P2', updated_at: at(-10) }),
  ]
  assert.deepEqual(
    sortTickets(list).map((t) => t.number),
    [4, 3, 2, 6, 5, 1, 8, 7],
  )
})

test('sort by time: newest first, whatever the priority', () => {
  const list = [
    ticket(1, { priority: 'P0', created_at: at(-300) }),
    ticket(2, { priority: 'P3', created_at: at(-10) }),
    ticket(3, { priority: 'P1', created_at: at(-60), status: 'resolved' }),
  ]
  assert.deepEqual(
    sortTickets(list, 'time').map((t) => t.number),
    [2, 3, 1],
  )
})

test('kanban: four columns in status order, each sorted', () => {
  const list = [
    ticket(1, { priority: 'P2' }),
    ticket(2, { priority: 'P0' }),
    ticket(3, { status: 'assigned', assignee_name: 'A' }),
    ticket(4, { status: 'resolved' }),
  ]
  const cols = kanbanColumns(list)
  assert.deepEqual(Object.keys(cols), ['open', 'assigned', 'human_active', 'resolved'])
  assert.deepEqual(
    cols.open.map((t) => t.number),
    [2, 1],
  )
  assert.deepEqual(
    cols.assigned.map((t) => t.number),
    [3],
  )
  assert.deepEqual(cols.human_active, [])
  assert.deepEqual(
    cols.resolved.map((t) => t.number),
    [4],
  )
})

test('visibility: only F5/F9 restricted, F0/F4/F5/F9/F10 never to the manager', () => {
  assert.equal(isRestricted({ category: 'F5' }), true)
  assert.equal(isRestricted({ category: 'F9' }), true)
  // Orchestrator's amendment (24.10): hidden from the manager only, HR reads them.
  for (const c of ['F0', 'F4', 'F10'] as const) {
    assert.equal(isRestricted({ category: c }), false)
    assert.equal(isRestricted({ category: c, restricted: false }), false)
  }
  // The server flag wins over the category.
  assert.equal(isRestricted({ category: 'question', restricted: true }), true)
  for (const c of ['F0', 'F4', 'F5', 'F9', 'F10'] as const) assert.equal(canNotifyManager(c), false)
  for (const c of ['F1', 'F2', 'F3', 'F6', 'F7', 'F8', 'question'] as const)
    assert.equal(canNotifyManager(c), true)
})

test('signature role follows the backend (route + HR-partner categories)', () => {
  assert.equal(signatureRole({ category: 'question', route: 'hr' }), 'hr')
  assert.equal(signatureRole({ category: 'question', route: 'recruiter' }), 'recruiter')
  assert.equal(signatureRole({ category: 'F4', route: 'hr' }), 'hrPartner')
  assert.equal(signatureRole({ category: 'F5', route: 'hr' }), 'hrPartner')
  assert.equal(signatureRole({ category: 'F9', route: 'security' }), 'security')
  assert.equal(signatureRole({ category: 'F2', route: 'hr' }), 'hr')
  assert.equal(signatureRole({ category: 'F7', route: 'recruiter' }), 'recruiter')
})

test("a newcomer's tickets, the latest first", () => {
  const list = [
    ticket(1, { created_at: at(-300) }),
    ticket(2, { newcomer_id: 'n2' }),
    ticket(3, { created_at: at(-5) }),
  ]
  assert.deepEqual(
    ticketsOfNewcomer(list, 'n1').map((t) => t.number),
    [3, 1],
  )
})

test('initials of the assignee', () => {
  assert.equal(initials('Смирнов А.'), 'СА')
  assert.equal(initials('Анна К.'), 'АК')
  assert.equal(initials('Дежурный СБ'), 'ДС')
  assert.equal(initials(null), '')
})

test('security view: F9 and restricted, hr_lead only, SLA one working day', () => {
  assert.deepEqual(viewsFor('hr_lead').includes('security'), true)
  assert.equal(viewsFor('recruiter').includes('security'), false)
  assert.equal(viewsFor(null).includes('security'), false)
  assert.equal(viewsFor('recruiter').includes('all'), true)
  const list = [
    ticket(1, { category: 'F9' }),
    ticket(2, { category: 'F5', restricted: true } as Partial<Ticket>),
    ticket(3, { category: 'F2' }),
    ticket(4, { category: 'F9', status: 'resolved' }),
  ]
  assert.deepEqual(
    applyView(list, 'security', NOW).map((t) => t.number),
    [1, 2, 4],
  )
  assert.equal(isSecurity({ category: 'F7' }), false)
  // Friday 12:00 -> Monday 12:00.
  const fri = Date.parse('2026-10-23T12:00:00+03:00')
  assert.equal(addWorkingDays(fri, 1), Date.parse('2026-10-26T12:00:00+03:00'))
  const old = ticket(5, { category: 'F9', created_at: at(-2 * 24 * 60) })
  const fresh = ticket(6, { category: 'F9', created_at: at(-60) })
  assert.equal(isSecurityOverdue(old, NOW), true)
  assert.equal(isSecurityOverdue(fresh, NOW), false)
  assert.equal(isSecurityOverdue({ ...old, status: 'resolved' }, NOW), false)
})
