// Order of the messages inside a block (src/lib/scenarioSteps.ts): "up /
// down", drag and drop, "insert after", a new day or time, positions.
import { test } from 'node:test'
import assert from 'node:assert/strict'
import {
  canMoveInBlock,
  createStep,
  insertAfter,
  moveInBlock,
  moveToStep,
  relocateStep,
  withPositions,
} from '../src/lib/scenarioSteps.ts'

// D0 10:00: a, b, c — one block; D0 12:00: d; D1 10:00: e, f.
function plan() {
  const at = (id: string, day: number, time: string) => ({ ...createStep(id, day), time })
  return [
    at('a', 0, '10:00'),
    at('b', 0, '10:00'),
    at('c', 0, '10:00'),
    at('d', 0, '12:00'),
    at('e', 1, '10:00'),
    at('f', 1, '10:00'),
  ]
}
const ids = (steps: { id: string }[]) => steps.map((s) => s.id).join('')

test('up and down swap neighbours inside the block', () => {
  assert.equal(ids(moveInBlock(plan(), 'b', -1)), 'bacdef')
  assert.equal(ids(moveInBlock(plan(), 'b', 1)), 'acbdef')
  assert.equal(ids(moveInBlock(plan(), 'e', 1)), 'abcdfe')
})

test('a step never leaves its block by the arrows', () => {
  const steps = plan()
  // c is last in D0 10:00, d is alone, e is first in D1.
  assert.equal(canMoveInBlock(steps, 'c', 1), false)
  assert.equal(moveInBlock(steps, 'c', 1), steps)
  assert.equal(canMoveInBlock(steps, 'd', -1), false)
  assert.equal(canMoveInBlock(steps, 'd', 1), false)
  assert.equal(canMoveInBlock(steps, 'e', -1), false)
  assert.equal(canMoveInBlock(steps, 'a', -1), false)
  assert.equal(canMoveInBlock(steps, 'f', 1), false)
  assert.equal(canMoveInBlock(steps, 'zz', 1), false)
})

test('drag and drop takes the target place, only inside one block', () => {
  assert.equal(ids(moveToStep(plan(), 'c', 'a')), 'cabdef')
  assert.equal(ids(moveToStep(plan(), 'a', 'c')), 'bcadef')
  const steps = plan()
  assert.equal(moveToStep(steps, 'a', 'd'), steps)
  assert.equal(moveToStep(steps, 'e', 'c'), steps)
  assert.equal(moveToStep(steps, 'a', 'a'), steps)
})

test('positions follow the new order and are sent with the list', () => {
  const saved = withPositions(moveInBlock(plan(), 'c', -1))
  assert.deepEqual(
    saved.map((s) => [s.id, s.position]),
    [
      ['a', 0],
      ['c', 1],
      ['b', 2],
      ['d', 3],
      ['e', 4],
      ['f', 5],
    ],
  )
})

test('insert after puts an empty message of the same block right after the step', () => {
  const steps = plan().map((s) => (s.id === 'a' ? { ...s, stage: 'week1' as const } : s))
  const r = insertAfter(steps, 'a', 'n')
  assert.ok(r)
  assert.equal(ids(r.steps), 'anbcdef')
  assert.equal(r.step.day_offset, 0)
  assert.equal(r.step.time, '10:00')
  assert.equal(r.step.stage, 'week1')
  assert.equal(r.step.kind, 'text')
  assert.equal(r.step.title, '')
  assert.equal(ids(insertAfter(plan(), 'c', 'n')!.steps), 'abcndef')
  assert.equal(insertAfter(plan(), 'zz', 'n'), null)
})

test('a new time moves the step to the end of the other block and says so', () => {
  const r = relocateStep(plan(), 'a', { time: '12:00' })
  assert.equal(ids(r.steps), 'bcdaef')
  assert.deepEqual(r.moved, {
    from: { day_offset: 0, time: '10:00', size: 3 },
    to: { day_offset: 0, time: '12:00', size: 2 },
    place: 2,
  })
})

test('a new day takes the step out of its block into plan order', () => {
  const r = relocateStep(plan(), 'b', { day_offset: 5 })
  assert.equal(ids(r.steps), 'acdefb')
  assert.equal(r.moved?.to.size, 1)
  assert.equal(r.moved?.from.size, 3)
  // A message joins block D1 10:00 at its end.
  const j = relocateStep(plan(), 'd', { day_offset: 1, time: '10:00' })
  assert.equal(ids(j.steps), 'abcefd')
  assert.equal(j.moved?.place, 3)
})

test('a lone step moved to a free day and time is not a block change', () => {
  const r = relocateStep(plan(), 'd', { time: '15:00' })
  assert.equal(ids(r.steps), 'abcdef')
  assert.equal(r.moved, null)
  const same = relocateStep(plan(), 'b', { time: '10:00', title: 'x' })
  assert.equal(ids(same.steps), 'abcdef')
  assert.equal(same.moved, null)
  assert.equal(same.steps[1].title, 'x')
})
