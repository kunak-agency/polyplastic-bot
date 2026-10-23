// Survey options as a list (src/lib/scenarioBranches.ts reorderOptions):
// branches, flags and rules keyed by the option number follow their option.
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { emptyBranch, optionKeys, reorderOptions } from '../src/lib/scenarioBranches.ts'
import type { Branch, StepSurvey } from '../src/types/api.ts'

function branch(text: string): Branch {
  const b = emptyBranch()
  b.messages[0].text_vy = text
  return b
}

function survey(): StepSurvey {
  return {
    scale: '1-10',
    threshold: 8,
    questions: [
      { text: 'Оценка', options: [] },
      {
        text: 'Что не так?',
        type: 'choice',
        // An option with a comma: the old comma-separated field split it in two.
        options: ['Пропуск, бейдж', 'Столовая', 'Наставник'],
        branches: { '1': branch('про пропуск'), '3': branch('про наставника') },
        option_flags: { '3': { code: 'F4', severity: 'high' } },
      },
    ],
    outcomes: [
      { if: [{ q: 2, in: [1, 3] }], messages: [], actions: [] },
      { if: [{ q: 1, in: [1, 2] }], messages: [], actions: [] },
    ],
  }
}

test('an option with a comma stays one option', () => {
  const s = survey()
  assert.deepEqual(
    optionKeys(s.questions[1], s.scale).map((o) => o.label),
    ['Пропуск, бейдж', 'Столовая', 'Наставник'],
  )
})

test('moving an option carries its branch, flag and rule along', () => {
  const s = survey()
  // Swap 1 and 2: "Столовая" first.
  const r = reorderOptions(s, 1, ['Столовая', 'Пропуск, бейдж', 'Наставник'], [1, 0, 2])
  const q = r.questions[1]
  assert.deepEqual(q.options, ['Столовая', 'Пропуск, бейдж', 'Наставник'])
  assert.deepEqual(Object.keys(q.branches ?? {}).sort(), ['2', '3'])
  assert.equal(q.branches?.['2'].messages[0].text_vy, 'про пропуск')
  assert.equal(q.branches?.['3'].messages[0].text_vy, 'про наставника')
  assert.deepEqual(q.option_flags, { '3': { code: 'F4', severity: 'high' } })
  assert.deepEqual(r.outcomes?.[0].if[0].in, [2, 3])
  // Rules about another question are untouched.
  assert.deepEqual(r.outcomes?.[1].if[0].in, [1, 2])
  // The input is not mutated.
  assert.equal(s.questions[1].branches?.['1'].messages[0].text_vy, 'про пропуск')
})

test('removing an option drops its keys and shifts the ones after it', () => {
  const s = survey()
  const r = reorderOptions(s, 1, ['Столовая', 'Наставник'], [1, 2])
  const q = r.questions[1]
  assert.deepEqual(q.options, ['Столовая', 'Наставник'])
  assert.deepEqual(Object.keys(q.branches ?? {}), ['2'])
  assert.equal(q.branches?.['2'].messages[0].text_vy, 'про наставника')
  assert.deepEqual(q.option_flags, { '2': { code: 'F4', severity: 'high' } })
  assert.deepEqual(r.outcomes?.[0].if[0].in, [2])
})

test('adding an option keeps the keys; removing the last keyed one drops the record', () => {
  const s = survey()
  const added = reorderOptions(s, 1, [...s.questions[1].options, 'Другое'], [0, 1, 2, -1])
  assert.deepEqual(Object.keys(added.questions[1].branches ?? {}).sort(), ['1', '3'])
  const noFlag = reorderOptions(s, 1, ['Пропуск, бейдж', 'Столовая'], [0, 1])
  assert.equal('option_flags' in noFlag.questions[1], false)
  assert.deepEqual(Object.keys(noFlag.questions[1].branches ?? {}), ['1'])
})

test('scale reasons are not keys: reordering them leaves score branches alone', () => {
  const s = survey()
  s.questions[0] = {
    text: 'Оценка',
    type: 'scale',
    options: ['Шумно', 'Далеко'],
    branches: { '1': branch('единица') },
  }
  const r = reorderOptions(s, 0, ['Далеко', 'Шумно'], [1, 0])
  assert.deepEqual(r.questions[0].options, ['Далеко', 'Шумно'])
  assert.deepEqual(Object.keys(r.questions[0].branches ?? {}), ['1'])
  assert.deepEqual(r.outcomes?.[1].if[0].in, [1, 2])
})
