// Overview rules (src/lib/overview.ts): drill-down targets, risk filter,
// heat map matrix with hidden cells, trend lines.
import { test } from 'node:test'
import assert from 'node:assert/strict'
import type { Overview, OverviewRiskZone } from '../src/types/api.ts'
import {
  actionTarget,
  filterRisks,
  heatColor,
  heatRows,
  hiddenCount,
  stagesTotal,
  trendLines,
  TILE_TARGETS,
  HEAT_RAMP,
} from '../src/lib/overview.ts'

function risk(n: number, overdue: boolean): OverviewRiskZone {
  return {
    newcomer_id: `n${n}`,
    full_name: `N${n}`,
    site: 'GTP',
    day: n,
    stage: 'month1',
    zone: 'yellow',
    reason: 'r',
    reasons: ['r'],
    overdue,
    last_pulse: null,
    open_tickets: 0,
  }
}

test('tiles and actions drill into the registry and the queue with a filter', () => {
  assert.deepEqual(TILE_TARGETS.startingThisWeek, {
    kind: 'route',
    name: 'newcomers',
    query: { chips: 'startsThisWeek' },
  })
  assert.deepEqual(actionTarget('tickets_sla_overdue'), {
    kind: 'route',
    name: 'tickets',
    query: { view: 'overdue' },
  })
  assert.deepEqual(actionTarget('tickets_unassigned'), {
    kind: 'route',
    name: 'tickets',
    query: { view: 'unassigned' },
  })
  assert.deepEqual(actionTarget('red_zones'), {
    kind: 'route',
    name: 'newcomers',
    query: { chips: 'red' },
  })
  assert.deepEqual(actionTarget('surveys_overdue'), { kind: 'overdue' })
  assert.equal(TILE_TARGETS.overdue.kind, 'overdue')
})

test('risk filter keeps only the overdue ones', () => {
  const list = [risk(1, true), risk(2, false), risk(3, true)]
  assert.deepEqual(
    filterRisks(list, true).map((r) => r.newcomer_id),
    ['n1', 'n3'],
  )
  assert.equal(filterRisks(list, false).length, 3)
})

test('stages total', () => {
  assert.equal(
    stagesTotal([
      { stage: 'before', count: 2 },
      { stage: 'week1', count: 3 },
    ]),
    5,
  )
})

const heatmap: Overview['heatmap'] = {
  min_group: 5,
  period_days: 30,
  sites: ['GrPP', 'GTP'],
  categories: [
    { code: 'F1', name: 'a' },
    { code: 'F2', name: 'b' },
  ],
  respondents: { GrPP: 4, GTP: 12 },
  cells: [
    { category: 'F1', site: 'GrPP', count: null, share: null, hidden: true },
    { category: 'F1', site: 'GTP', count: 0, share: 0, hidden: false },
    { category: 'F2', site: 'GTP', count: 6, share: 0.5, hidden: false },
  ],
}

test('heat map rows follow server order; a missing cell is hidden, not zero', () => {
  const rows = heatRows(heatmap)
  assert.deepEqual(
    rows.map((r) => r.code),
    ['F1', 'F2'],
  )
  assert.equal(rows[1].cells[0].hidden, true)
  assert.equal(rows[1].cells[0].count, null)
  assert.equal(rows[1].cells[1].count, 6)
  assert.equal(hiddenCount(heatmap), 1)
})

test('heat colour: zero and hidden are neutral, more share is darker', () => {
  assert.equal(heatColor(null), heatColor(0))
  assert.ok(HEAT_RAMP.includes(heatColor(0.1)))
  assert.ok(HEAT_RAMP.indexOf(heatColor(0.6)) > HEAT_RAMP.indexOf(heatColor(0.1)))
})

test('trend lines align to weeks and keep gaps for hidden points', () => {
  const lines = trendLines({
    weeks: ['2026-10-12', '2026-10-19'],
    min_group: 5,
    series: [
      {
        site: 'GTP',
        points: [
          { week: '2026-10-12', avg: null, respondents: 3, hidden: true },
          { week: '2026-10-19', avg: 7.1, respondents: 9, hidden: false },
        ],
      },
    ],
  })
  assert.deepEqual(lines[0].values, [null, 7.1])
  assert.equal(lines[0].hidden, 1)
})

test('day label', async () => {
  const { dayLabel } = await import('../src/lib/overview.ts')
  assert.equal(dayLabel(0), 'D0')
  assert.equal(dayLabel(12), 'D+12')
  assert.equal(dayLabel(-3), 'D−3')
})
