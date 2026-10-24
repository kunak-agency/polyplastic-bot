// Role dashboards config (src/lib/dashboard.ts).
import { test } from 'node:test'
import assert from 'node:assert/strict'
import type { DashTile, DashboardItem, TodayRow } from '../src/types/api.ts'
import {
  HR_LEAD_TILES,
  RECRUITER_TILES,
  TODAY_ACTIONS,
  TODAY_LIMIT,
  chipsFor,
  isQuiet,
  share,
  sortLoad,
  sortToday,
  tileById,
  tileIds,
  tileText,
} from '../src/lib/dashboard.ts'

function item(id: string, extra: Partial<DashboardItem> = {}): DashboardItem {
  return {
    newcomer_id: id,
    zone: null,
    silent_days: null,
    catchup: null,
    ip_decision_date: null,
    tickets_open: 0,
    tickets_sla_overdue: 0,
    ...extra,
  }
}

function row(id: string, extra: Partial<TodayRow> = {}): TodayRow {
  return {
    id,
    kind: 'silent',
    zone: 'yellow',
    reason: 'r',
    newcomer_id: id,
    newcomer_name: id,
    ticket_id: null,
    due_at: null,
    day: 0,
    actions: ['open'],
    ...extra,
  }
}

test('six tiles per role, unique ids', () => {
  for (const cfg of [RECRUITER_TILES, HR_LEAD_TILES]) {
    assert.equal(cfg.length, 6)
    assert.equal(new Set(cfg.map((t) => t.id)).size, 6)
  }
})

test('a tile filters by its ids; pair tiles show both numbers', () => {
  const tiles: Record<string, DashTile> = {
    catching_up: { value: 1, red: false, ids: ['a'] },
    sla: { value: 2, secondary: 1, red: true },
  }
  const catching = tileById(RECRUITER_TILES, 'catching_up')
  const sla = tileById(RECRUITER_TILES, 'sla')
  assert.ok(catching && sla)
  assert.deepEqual([...(tileIds(tiles, catching) ?? [])], ['a'])
  assert.equal(tileIds(tiles, sla), null)
  assert.equal(tileText(tiles.sla, sla), '2 / 1')
})

test('today: red first, then deadline, at most 10 rows', () => {
  const rows = [
    row('y1', { due_at: '2026-10-24T10:00:00Z' }),
    row('r2', { zone: 'red', due_at: '2026-10-25T10:00:00Z' }),
    row('r1', { zone: 'red', due_at: '2026-10-24T09:00:00Z' }),
  ]
  assert.deepEqual(
    sortToday(rows).map((r) => r.id),
    ['r1', 'r2', 'y1'],
  )
  const many = Array.from({ length: 15 }, (_, i) => row(`x${i}`))
  assert.equal(sortToday(many).length, TODAY_LIMIT)
})

test('actions: contact opens the card with the contact panel, take needs a ticket', () => {
  const r = row('a')
  assert.deepEqual(TODAY_ACTIONS.contact.target(r), {
    kind: 'go',
    to: { name: 'newcomer', params: { id: 'a' }, query: { contact: '1' } },
  })
  assert.equal(TODAY_ACTIONS.take.target(r), null)
  assert.deepEqual(TODAY_ACTIONS.take.target({ ...r, ticket_id: 't' }), {
    kind: 'call',
    call: 'take',
  })
})

test('chips: silent days and catch-up progress', () => {
  const chips = chipsFor(item('a', { silent_days: 3, catchup: { done: 1, total: 4 } }))
  assert.deepEqual(
    chips.map((c) => [c.key, c.params]),
    [
      ['silent', { n: 3 }],
      ['catchup', { done: 1, total: 4 }],
    ],
  )
})

test('small numbers: few under 5, counts under 20, percent from 20', () => {
  assert.deepEqual(share(2, 4), { kind: 'few' })
  assert.deepEqual(share(12, 15), { kind: 'count', part: 12, total: 15 })
  assert.deepEqual(share(16, 20), { kind: 'percent', value: 80 })
})

test('quiet when nothing to do and all tiles are zero', () => {
  assert.equal(isQuiet([], { a: { value: 0, red: false } }), true)
  assert.equal(isQuiet([], { a: { value: 1, red: false } }), false)
  assert.equal(isQuiet([row('a')], {}), false)
})

test('load: sorted by overdue SLA, no ranking by speed', () => {
  const rows = [
    { full_name: 'B', tickets_sla_overdue: 0, tickets_open: 5 },
    { full_name: 'A', tickets_sla_overdue: 2, tickets_open: 1 },
  ]
  assert.deepEqual(
    sortLoad(rows).map((r) => r.full_name),
    ['A', 'B'],
  )
})
