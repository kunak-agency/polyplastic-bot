// Digest screen rules (src/lib/digest.ts): ISO weeks, signed deltas, bars,
// pulse levels of the head-of-unit cards.
import { test } from 'node:test'
import assert from 'node:assert/strict'
import {
  delta,
  isoWeek,
  pulseLevel,
  shiftWeek,
  signed,
  themeBars,
  trendOf,
  weekMonday,
} from '../src/lib/digest.ts'
import type { DigestTheme } from '../src/types/api.ts'

test('isoWeek matches ISO-8601 (week 1 holds the first Thursday)', () => {
  assert.equal(isoWeek(new Date(2026, 9, 19)), '2026-W43')
  assert.equal(isoWeek(new Date(2026, 9, 25)), '2026-W43')
  assert.equal(isoWeek(new Date(2027, 0, 1)), '2026-W53')
  assert.equal(isoWeek(new Date(2025, 11, 29)), '2026-W01')
})

test('weekMonday and shiftWeek walk across years', () => {
  const mon = weekMonday('2026-W43')
  assert.ok(mon)
  assert.equal(mon.getDate(), 19)
  assert.equal(weekMonday('2026-43'), null)
  assert.equal(weekMonday('2025-W53'), null)
  assert.equal(shiftWeek('2026-W43', -1), '2026-W42')
  assert.equal(shiftWeek('2026-W01', -1), '2025-W52')
  assert.equal(shiftWeek('2026-W53', 1), '2027-W01')
})

test('signed and delta keep null as "no comparison"', () => {
  assert.equal(signed(4), '+4')
  assert.equal(signed(-3), '−3')
  assert.equal(signed(0), '0')
  assert.equal(signed(null), null)
  assert.equal(delta(81, 84), -3)
  assert.equal(delta(null, 84), null)
  assert.equal(trendOf(-1), 'down')
  assert.equal(trendOf(null), 'none')
})

test('themeBars is relative to the largest theme', () => {
  const th = (n: number) => ({ newcomers: n }) as DigestTheme
  assert.deepEqual(themeBars([th(10), th(5), th(1)]), [100, 50, 10])
  assert.deepEqual(themeBars([]), [])
})

test('pulseLevel buckets the last pulse', () => {
  assert.equal(pulseLevel(9), 'good')
  assert.equal(pulseLevel(6), 'mid')
  assert.equal(pulseLevel(3), 'low')
  assert.equal(pulseLevel(null), 'none')
})
