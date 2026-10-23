// Plan preview of the launch dialog (src/lib/calendar.ts): dates of steps from
// the start date, moved off weekends and Friday evenings.
import { test } from 'node:test'
import assert from 'node:assert/strict'
import {
  addDays,
  buildPlanPreview,
  daysBetween,
  formatDayOffset,
  resolveFireDate,
  weekStart,
  type PreviewStepInput,
} from '../src/lib/calendar.ts'

// 2026-11-02 is a Monday.
const MONDAY = '2026-11-02'

function step(id: string, day: number, time = '10:00', extra: Partial<PreviewStepInput> = {}) {
  return {
    id,
    day_offset: day,
    time,
    sites: [],
    kind: 'text',
    title: id,
    work_hours_only: true,
    ...extra,
  } satisfies PreviewStepInput
}

test('calendar arithmetic crosses months and years', () => {
  assert.equal(addDays('2026-10-30', 3), '2026-11-02')
  assert.equal(addDays('2026-12-30', 3), '2027-01-02')
  assert.equal(addDays(MONDAY, -3), '2026-10-30')
  assert.equal(daysBetween('2026-10-30', MONDAY), 3)
  assert.equal(daysBetween(MONDAY, '2026-10-30'), -3)
  assert.equal(weekStart('2026-11-08'), MONDAY)
  assert.equal(weekStart(MONDAY), MONDAY)
})

test('day offsets read as in the spec', () => {
  assert.equal(formatDayOffset(0), 'D0')
  assert.equal(formatDayOffset(14), 'D+14')
  assert.equal(formatDayOffset(-3), 'D−3')
})

test('a weekday step stays where it is', () => {
  assert.deepEqual(resolveFireDate(MONDAY, -3, '10:00', true), {
    planned: '2026-10-30',
    date: '2026-10-30',
    shifted: false,
  })
})

test('a weekend step moves to Monday', () => {
  assert.equal(resolveFireDate(MONDAY, -1, '10:00', true).date, MONDAY)
  assert.equal(resolveFireDate(MONDAY, -2, '10:00', false).date, MONDAY)
  assert.equal(resolveFireDate(MONDAY, 5, '10:00', true).date, '2026-11-09')
})

test('Friday after 17:00 moves only when working hours are required', () => {
  assert.equal(resolveFireDate(MONDAY, 4, '17:30', true).date, '2026-11-09')
  assert.equal(resolveFireDate(MONDAY, 4, '17:30', false).date, '2026-11-06')
  assert.equal(resolveFireDate(MONDAY, 4, '17:00', true).date, '2026-11-06')
})

test('preview groups by day, sorts by time and counts moved steps', () => {
  const p = buildPlanPreview(
    [step('b', 0, '12:00'), step('a', 0, '10:00'), step('c', -1), step('d', -3)],
    MONDAY,
    'GTP',
  )
  assert.deepEqual(
    p.days.map((d) => [d.day_offset, d.planned, d.movedTo, d.steps.map((s) => s.id)]),
    [
      [-3, '2026-10-30', null, ['d']],
      [-1, '2026-11-01', MONDAY, ['c']],
      [0, MONDAY, null, ['a', 'b']],
    ],
  )
  assert.equal(p.total, 4)
  assert.equal(p.shiftedSteps, 1)
})

test('preview keeps only the steps of the chosen site', () => {
  const steps = [
    step('all', 0),
    step('gtp', 2, '11:00', { sites: ['GTP'] }),
    step('office', 10, '10:00', { sites: ['GrPP', 'Luzhniki'] }),
  ]
  const ids = (site: 'GTP' | 'GrPP' | null) =>
    buildPlanPreview(steps, MONDAY, site).days.flatMap((d) => d.steps.map((s) => s.id))
  assert.deepEqual(ids('GTP'), ['all', 'gtp'])
  assert.deepEqual(ids('GrPP'), ['all', 'office'])
  assert.deepEqual(ids(null), ['all', 'gtp', 'office'])
})
