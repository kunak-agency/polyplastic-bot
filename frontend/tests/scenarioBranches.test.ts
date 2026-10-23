// Branch validation and block grouping (src/lib/scenarioBranches.ts).
import { test } from 'node:test'
import assert from 'node:assert/strict'
import {
  branchCount,
  branchFor,
  countIssues,
  effectiveAdvance,
  emptyBranch,
  groupBlocks,
  hasErrors,
  statsBadge,
  labelIsLong,
  LABEL_WARN,
  validateScenario,
  validateStep,
} from '../src/lib/scenarioBranches.ts'
import type { ScenarioStep, SiteCode } from '../src/types/api.ts'

const ALL: SiteCode[] = ['GrPP', 'Luzhniki', 'GTP']

function step(id: string, day: number, time: string, p: Partial<ScenarioStep> = {}): ScenarioStep {
  return {
    id,
    day_offset: day,
    time,
    stage: 'before',
    sites: [],
    kind: 'text',
    title: id,
    text_ty: 'ty',
    text_vy: 'vy',
    buttons: [],
    survey: null,
    condition: null,
    work_hours_only: true,
    ...p,
  }
}

const ctx = { gotoTargets: new Set<string>(['blk-1']) }

function codes(s: ScenarioStep) {
  return validateStep(s, ctx).map((i) => `${i.level}:${i.code}`)
}

test('blocks group consecutive steps with the same day and time', () => {
  const blocks = groupBlocks([
    step('a', -3, '10:00', { key: 'blk-1' }),
    step('b', -3, '10:00'),
    step('c', -3, '12:00'),
    step('d', 0, '10:00'),
    step('e', 0, '10:00'),
  ])
  assert.deepEqual(
    blocks.map((b) => [b.key, b.steps.map((s) => s.id)]),
    [
      ['blk-1', ['a', 'b']],
      ['-3|12:00', ['c']],
      ['0|10:00', ['d', 'e']],
    ],
  )
})

test('a clean step with a filled branch has no issues', () => {
  const branch = emptyBranch()
  branch.messages[0].text_vy = 'Передадим HR'
  branch.actions.push({ type: 'handoff', to: 'hr' })
  const s = step('a', 1, '10:00', {
    buttons: [
      { label: 'Всё есть', action: '' },
      { label: 'Чего-то нет', action: '', branch },
    ],
  })
  assert.deepEqual(codes(s), [])
  assert.deepEqual(branchCount(s), { withBranch: 1, total: 2 })
})

test('errors: empty label, more than 7 buttons', () => {
  const many = Array.from({ length: 8 }, (_, i) => ({ label: `B${i}`, action: '' }))
  assert.deepEqual(codes(step('a', 1, '10:00', { buttons: many })), ['error:tooManyButtons'])
  const seven = Array.from({ length: 7 }, (_, i) => ({ label: `B${i}`, action: '' }))
  assert.deepEqual(codes(step('a', 1, '10:00', { buttons: seven })), [])
  const s = step('b', 1, '10:00', { buttons: [{ label: ' ', action: '' }] })
  assert.deepEqual(codes(s), ['error:labelEmpty'])
})

test('a label over 24 characters is a warning, not an error (MAX has no limit)', () => {
  assert.equal(LABEL_WARN, 24)
  // The old 18-character "limit" is gone: a 22-character label is clean.
  const s = step('b', 1, '10:00', { buttons: [{ label: 'Передать через Корсика', action: '' }] })
  assert.deepEqual(codes(s), [])
  const exactly24 = 'а'.repeat(24)
  const over = 'Да, расскажи про столовую' // 25
  assert.equal(labelIsLong(exactly24), false)
  assert.equal(labelIsLong(over), true)
  // Characters, not UTF-16 units: an emoji counts once.
  assert.equal(labelIsLong('😀'.repeat(24)), false)
  const long = step('c', 1, '10:00', {
    buttons: [
      { label: 'Да', action: '' },
      { label: over, action: '' },
    ],
  })
  assert.deepEqual(codes(long), ['warning:labelTooLong'])
  assert.equal(validateStep(long, ctx)[0].button, 1)
  assert.equal(hasErrors([validateStep(long, ctx)]), false)
})

test('errors: handoff without a recipient, goto nowhere; terminal buttons are checked too', () => {
  const b = emptyBranch()
  b.messages[0].text_vy = 'x'
  b.messages[0].buttons = [{ label: 'Очень длинная подпись кнопки', action: '' }]
  b.actions.push({ type: 'handoff', to: '' })
  b.then = 'goto_block'
  b.goto = 'blk-404'
  const s = step('a', 1, '10:00', { buttons: [{ label: 'Да', action: '', branch: b }] })
  assert.deepEqual(codes(s), ['warning:labelTooLong', 'error:handoffNoTarget', 'error:gotoNowhere'])
  b.goto = 'blk-1'
  b.actions[0].to = 'recruiter'
  b.messages[0].buttons = []
  assert.deepEqual(codes(s), [])
})

test('warnings: empty branch, media without a file', () => {
  const s = step('a', 1, '10:00', {
    media: [{ name: 'Изображение 14' }, { name: 'Памятка', media_id: 'm1' }],
    buttons: [{ label: 'Да', action: '', branch: emptyBranch() }],
  })
  assert.deepEqual(codes(s), ['warning:mediaMissing', 'warning:branchEmpty'])
  // The library knows better than media_id: a "ready" entry is fine.
  const lib = { ...ctx, mediaStatus: new Map([['Изображение 14', 'ready' as const]]) }
  assert.deepEqual(
    validateStep(s, lib).map((i) => i.code),
    ['branchEmpty'],
  )
  assert.equal(hasErrors([validateStep(s, ctx)]), false)
})

test('block warnings: a missing site variant and three "auto" in a row', () => {
  const steps = [
    step('g', 2, '12:00', { sites: ['GrPP'] }),
    step('l', 2, '12:00', { sites: ['Luzhniki'] }),
    step('single', 3, '09:00', { sites: ['GTP'] }),
    step('a1', 5, '10:00', { advance: 'auto' }),
    step('a2', 5, '10:00', { advance: 'auto' }),
    step('a3', 5, '10:00', { advance: 'auto' }),
    step('q', 5, '10:00', { buttons: [{ label: 'Ок', action: '' }] }),
    step('b1', 6, '10:00', { advance: 'auto' }),
    step('b2', 6, '10:00', { advance: 'auto' }),
    step('b3', 6, '10:00'),
  ]
  const all = validateScenario(steps, [], ALL)
  assert.deepEqual(
    all.get('g')?.map((i) => [i.code, i.detail]),
    [['siteVariantMissing', 'GTP']],
  )
  // One site-specific step is a condition, not a variant set.
  assert.deepEqual(all.get('single'), [])
  for (const id of ['a1', 'a2', 'a3'])
    assert.deepEqual(
      all.get(id)?.map((i) => i.code),
      ['autoBurst'],
    )
  assert.deepEqual(all.get('b1'), [])
  assert.deepEqual(countIssues(all.values()), { errors: 0, warnings: 5 })
  // An office-only scenario does not ask for GTP.
  assert.deepEqual(validateScenario(steps.slice(0, 2), ['GrPP', 'Luzhniki'], ALL).get('g'), [])
})

test('"Next" is the default between steps without a question', () => {
  assert.equal(effectiveAdvance(step('a', 0, '10:00'), true), 'button')
  assert.equal(effectiveAdvance(step('a', 0, '10:00', { advance: 'auto' }), true), 'auto')
  assert.equal(effectiveAdvance(step('a', 0, '10:00'), false), 'none')
  const asks = step('a', 0, '10:00', { buttons: [{ label: 'Да', action: '' }] })
  assert.equal(effectiveAdvance(asks, true), 'none')
})

test('badges respect the threshold', () => {
  assert.equal(statsBadge({ share: null, picked: 3 }), null)
  assert.deepEqual(statsBadge({ share: 0.3125, picked: 12 }), { percent: 31, picked: 12 })
})

test('a shared branch answers every button in "on"', () => {
  const shared = { ...emptyBranch(), on: [0, 1] }
  shared.messages[0].text_vy = 'Отлично!'
  const s = step('a', 0, '10:00', {
    buttons: [
      { label: '1', action: '', branch: shared },
      { label: '2', action: '' },
      { label: '3', action: '' },
    ],
  })
  assert.equal(branchFor(s, 1)?.owner, 0)
  assert.equal(branchFor(s, 2), null)
  assert.deepEqual(branchCount(s), { withBranch: 2, total: 3 })
})

test('survey option branches and outcomes are validated like button branches', () => {
  const s = step('a', 3, '17:00', {
    kind: 'survey',
    survey: {
      scale: 'choice',
      threshold: null,
      questions: [
        {
          text: 'Как встретили?',
          options: ['Хорошо', 'Никак'],
          branches: {
            '2': { messages: [], actions: [{ type: 'handoff', to: '' }], then: 'continue' },
          },
          option_flags: { '2': { code: 'F4', severity: 'medium' } },
        },
      ],
      outcomes: [
        {
          if: [{ q: 1, in: [2] }],
          match: 'any',
          group: 'a',
          messages: [],
          actions: [{ type: 'handoff', to: '' }],
        },
      ],
    },
  })
  const got = validateStep(s, ctx)
  assert.deepEqual(
    got.map((i) => [i.code, i.question]),
    [
      ['handoffNoTarget', 0],
      ['handoffNoTarget', undefined],
    ],
  )
})
