// Registry chips and sorting (src/lib/newcomerFilters.ts).
import { test } from 'node:test'
import assert from 'node:assert/strict'
import type { Newcomer } from '../src/types/api.ts'
import {
  adaptationProgress,
  applyChips,
  chipCounts,
  matchesChip,
  sortNewcomers,
} from '../src/lib/newcomerFilters.ts'

// Wednesday 2026-11-04, midday local time.
const NOW = new Date(2026, 10, 4, 12, 0, 0)

function person(id: string, extra: Partial<Newcomer> = {}): Newcomer {
  return {
    id,
    full_name: id,
    phone: '+70000000000',
    site: 'GTP',
    department: 'd',
    position: 'p',
    manager_name: 'm',
    mentor_name: 'n',
    recruiter_name: 'r',
    start_date: '2026-10-01',
    shift: null,
    scenario_id: 's',
    scenario_version: 1,
    address_form: 'vy',
    consent: 'given',
    connection: 'connected',
    day: 34,
    stage: 'probation',
    invite_link: 'x',
    last_activity_at: new Date(2026, 10, 4, 9).toISOString(),
    last_pulse: 4,
    risk: 'green',
    risk_reason: null,
    open_tickets: 0,
    ...extra,
  }
}

test('"starts this week" is Monday to Sunday of the current week', () => {
  assert.ok(matchesChip(person('a', { start_date: '2026-11-02' }), 'startsThisWeek', NOW))
  assert.ok(matchesChip(person('b', { start_date: '2026-11-08' }), 'startsThisWeek', NOW))
  assert.ok(!matchesChip(person('c', { start_date: '2026-11-09' }), 'startsThisWeek', NOW))
  assert.ok(!matchesChip(person('d', { start_date: '2026-11-01' }), 'startsThisWeek', NOW))
})

test('"silent" means connected and quiet for more than three days', () => {
  const old = new Date(2026, 9, 31, 11).toISOString() // 4 days + 1 h ago
  const recent = new Date(2026, 10, 1, 13).toISOString() // under 3 days ago
  assert.ok(matchesChip(person('a', { last_activity_at: old }), 'silent', NOW))
  assert.ok(matchesChip(person('b', { last_activity_at: null }), 'silent', NOW))
  assert.ok(!matchesChip(person('c', { last_activity_at: recent }), 'silent', NOW))
  assert.ok(
    !matchesChip(person('d', { connection: 'invited', last_activity_at: null }), 'silent', NOW),
  )
  assert.ok(
    !matchesChip(person('e', { connection: 'stopped', last_activity_at: old }), 'silent', NOW),
  )
})

test('chips narrow together and counts are per chip', () => {
  const list = [
    person('red-new', { risk: 'red', start_date: '2026-11-03', connection: 'invited' }),
    person('red-old', { risk: 'red' }),
    person('fresh', { start_date: '2026-11-05', connection: 'invited' }),
  ]
  assert.deepEqual(
    applyChips(list, ['red'], NOW).map((n) => n.id),
    ['red-new', 'red-old'],
  )
  assert.deepEqual(
    applyChips(list, ['red', 'startsThisWeek'], NOW).map((n) => n.id),
    ['red-new'],
  )
  assert.equal(applyChips(list, [], NOW).length, 3)
  assert.deepEqual(chipCounts(list, NOW), {
    startsThisWeek: 2,
    silent: 0,
    red: 2,
    notConnected: 2,
  })
})

test('sorting by risk puts red first, then the furthest along', () => {
  const list = [
    person('g', { risk: 'green', day: 50 }),
    person('y', { risk: 'yellow', day: 5 }),
    person('r1', { risk: 'red', day: 3 }),
    person('r2', { risk: 'red', day: 12 }),
  ]
  assert.deepEqual(
    sortNewcomers(list, 'risk').map((n) => n.id),
    ['r2', 'r1', 'y', 'g'],
  )
  assert.deepEqual(
    sortNewcomers(list, 'day').map((n) => n.id),
    ['g', 'r2', 'y', 'r1'],
  )
})

test('progress spans D-3..D+90 and is clamped', () => {
  assert.equal(adaptationProgress(-3), 0)
  assert.equal(adaptationProgress(-10), 0)
  assert.equal(adaptationProgress(90), 1)
  assert.equal(adaptationProgress(120), 1)
  assert.ok(Math.abs(adaptationProgress(43.5) - 0.5) < 1e-9)
})
