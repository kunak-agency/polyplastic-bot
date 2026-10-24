// "Indicators" screen rules (src/lib/indicators.ts): period from the query,
// drill-down targets, the "versus the previous period" line, small sites.
import { test } from 'node:test'
import assert from 'node:assert/strict'
import type { IndicatorTile, Indicators } from '../src/types/api.ts'
import {
  lossTarget,
  parsePeriod,
  smallSites,
  splitMinutes,
  tileTarget,
  todayTarget,
  unitValue,
  vsPrev,
} from '../src/lib/indicators.ts'

function tile(over: Partial<IndicatorTile>): IndicatorTile {
  return {
    key: 'pulse_response',
    status: 'ok',
    unit: 'pct',
    value: 80,
    num: 8,
    n: 10,
    prev: 50,
    prev_n: 10,
    delta: 30,
    direction: 'up',
    demo: false,
    ...over,
  }
}

test('period comes from ?period=, anything else is 30 days', () => {
  assert.equal(parsePeriod('90d'), '90d')
  assert.equal(parsePeriod('q'), 'q')
  assert.equal(parsePeriod('week'), '30d')
  assert.equal(parsePeriod(undefined), '30d')
  assert.equal(parsePeriod(['q']), '30d')
})

test('drill-downs use only filters the lists already read', () => {
  assert.deepEqual(tileTarget('connected_before_d0'), {
    name: 'newcomers',
    query: { chips: 'notConnected' },
  })
  assert.equal(tileTarget('pulse_response'), null)
  assert.equal(tileTarget('ip_passed'), null)
  assert.deepEqual(lossTarget('silent'), { name: 'newcomers', query: { chips: 'silent' } })
  assert.equal(lossTarget('stopped'), null)
  assert.equal(lossTarget('connected_late'), null)
  assert.equal(todayTarget(true), 'dashboard')
  assert.equal(todayTarget(false), 'tickets')
})

test('versus the previous period in words', () => {
  assert.deepEqual(vsPrev(tile({})), { key: 'up', d: 30 })
  assert.deepEqual(vsPrev(tile({ delta: -4, direction: 'down' })), { key: 'down', d: 4 })
  assert.deepEqual(vsPrev(tile({ delta: 1, direction: 'same' })), { key: 'same', d: 1 })
  assert.deepEqual(vsPrev(tile({ prev: null, delta: null, direction: null })), {
    key: 'none',
    d: 0,
  })
  assert.deepEqual(vsPrev(tile({ unit: 'min', delta: -12, direction: 'down' })), {
    key: 'downMin',
    d: 12,
  })
  assert.equal(vsPrev(tile({ status: 'few', value: null })), null)
})

test('minutes and units', () => {
  assert.deepEqual(splitMinutes(95), { h: 1, m: 35 })
  assert.deepEqual(splitMinutes(42.4), { h: 0, m: 42 })
  assert.equal(unitValue(1.25, 'per10'), '1,3')
  assert.equal(unitValue(81.6, 'pct'), '82')
})

test('a site whose every metric cell is hidden is "small"', () => {
  const cell = (site: 'GrPP' | 'Luzhniki' | 'GTP', hidden: boolean) => ({
    site,
    value: hidden ? null : 50,
    n: hidden ? 3 : 20,
    hidden,
    diff: null,
    diff_pp: null,
    verdict: hidden ? ('hidden' as const) : ('noise' as const),
    flag: false,
  })
  const data = {
    sites: {
      sites: ['GrPP', 'Luzhniki', 'GTP'],
      cohorts: {},
      flagged: [],
      rows: [
        {
          key: 'cohort',
          unit: 'count',
          avg: 26,
          avg_n: 26,
          cells: [cell('GrPP', false), cell('Luzhniki', false), cell('GTP', false)],
        },
        {
          key: 'pulse_response',
          unit: 'pct',
          avg: 50,
          avg_n: 40,
          cells: [cell('GrPP', false), cell('Luzhniki', true), cell('GTP', false)],
        },
      ],
    },
  } as unknown as Indicators
  assert.deepEqual([...smallSites(data)], ['Luzhniki'])
})
