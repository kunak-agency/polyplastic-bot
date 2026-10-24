// Question types of a survey (src/lib/scenarioSteps.ts): the type lives on the
// question; a question of the old format keeps the survey's scale.
import { test } from 'node:test'
import assert from 'node:assert/strict'
import {
  createStep,
  defaultSurvey,
  newQuestion,
  questionType,
  stepIssues,
  textToFirstQuestion,
  typedQuestion,
} from '../src/lib/scenarioSteps.ts'
import type { StepSurvey, SurveyQuestion } from '../src/types/api.ts'

const legacy = (scale: StepSurvey['scale'], threshold: number | null = 7): StepSurvey => ({
  scale,
  threshold,
  questions: [{ text: 'q', options: [] }],
})

test('a question without type takes it from the survey scale', () => {
  assert.equal(questionType({}, '1-10'), 'scale')
  assert.equal(questionType({}, '1-5'), 'scale')
  assert.equal(questionType({}, 'yesno'), 'yesno')
  assert.equal(questionType({}, 'choice'), 'choice')
  assert.equal(questionType({ type: 'text' }, 'choice'), 'text')
  assert.equal(questionType({ type: 'multi' }, '1-10'), 'multi')
})

test('picking a type keeps the scale and threshold the old question had', () => {
  const sv = legacy('1-5', 3)
  const q = typedQuestion(sv.questions[0], 'scale', sv)
  assert.deepEqual(q, {
    text: 'q',
    options: [],
    type: 'scale',
    scale: '1-5',
    threshold: 3,
    comment: 'below',
  })
  const own = typedQuestion({ text: 'q', options: [], threshold: 5 }, 'scale', legacy('1-10'))
  assert.equal(own.threshold, 5)
  const yn = typedQuestion(q, 'yesno', sv)
  assert.equal(yn.type, 'yesno')
  assert.equal(yn.comment, 'on_no')
  assert.equal('scale' in yn, false)
  assert.equal('threshold' in yn, false)
  assert.equal(typedQuestion(q, 'choice', legacy('yesno')).comment, '')
})

test('a new question is typed like the one before it', () => {
  assert.deepEqual(newQuestion(null), {
    text: '',
    options: [],
    type: 'scale',
    scale: '1-10',
    threshold: 8,
    comment: 'below',
  })
  // After an old-format question: its effective type, made explicit.
  const afterLegacy = newQuestion(legacy('choice'))
  assert.equal(afterLegacy.type, 'choice')
  assert.equal(afterLegacy.text, '')
  const afterLegacyScale = newQuestion(legacy('1-5', 4))
  assert.equal(afterLegacyScale.scale, '1-5')
  assert.equal(afterLegacyScale.threshold, 4)
  // After a typed one: its scale, threshold and comment.
  const prev: SurveyQuestion = {
    text: 'p',
    options: ['x'],
    type: 'scale',
    scale: '1-5',
    threshold: 2,
    comment: 'optional',
  }
  const n = newQuestion({ scale: '1-10', threshold: 8, questions: [prev] })
  assert.deepEqual(n, {
    text: '',
    options: [],
    type: 'scale',
    scale: '1-5',
    threshold: 2,
    comment: 'optional',
  })
  assert.equal(
    newQuestion({ scale: '1-10', threshold: 8, questions: [{ ...prev, type: 'multi' }] }).type,
    'multi',
  )
})

test('the survey check reads the type of each question', () => {
  const step = (survey: StepSurvey) => ({
    ...createStep('s', 1),
    title: 't',
    kind: 'survey' as const,
    survey,
  })
  // Old format "choice": two options needed; a typed scale in it does not.
  assert.deepEqual(stepIssues(step(legacy('choice'))), ['surveyQuestions'])
  const mixed: StepSurvey = {
    scale: 'choice',
    threshold: null,
    questions: [{ text: 'q', options: [], type: 'scale', scale: '1-10' }],
  }
  assert.deepEqual(stepIssues(step(mixed)), [])
  const multi: StepSurvey = {
    scale: '1-10',
    threshold: 8,
    questions: [{ text: 'q', options: ['a'], type: 'multi' }],
  }
  assert.deepEqual(stepIssues(step(multi)), ['surveyQuestions'])
})

test('a survey text is an optional intro; question 1 starts empty', () => {
  const base = { ...createStep('s', 1), title: 't', kind: 'survey' as const }
  const s = { ...base, text_ty: 'Как прошла неделя?', text_vy: 'Как прошла неделя?' }
  const withSurvey = { ...s, survey: defaultSurvey() }
  assert.equal(withSurvey.text_ty, 'Как прошла неделя?')
  assert.equal(withSurvey.survey.questions[0].text, '')
  // No intro at all is fine for a survey; one form only is not.
  const ok = {
    ...base,
    survey: { ...defaultSurvey(), questions: [{ ...newQuestion(null), text: 'q' }] },
  }
  assert.deepEqual(stepIssues(ok), [])
  assert.deepEqual(stepIssues({ ...ok, text_ty: 'x' }), ['noText'])
})

test('"make the text question 1" moves the text and clears the intro', () => {
  const step = {
    ...createStep('s', 1),
    kind: 'survey' as const,
    text_ty: ' Оцени неделю ',
    text_vy: 'Оцените неделю',
    survey: defaultSurvey(),
  }
  const p = textToFirstQuestion(step)
  assert.ok(p)
  assert.equal(p.text_ty, '')
  assert.equal(p.text_vy, '')
  assert.equal(p.survey?.questions[0].text, 'Оцени неделю')
  assert.equal(p.survey?.questions[0].text_vy, 'Оцените неделю')
  assert.equal(p.survey?.questions[0].type, 'scale')
  // Same text on both forms: no separate "vy" question.
  const same = textToFirstQuestion({ ...step, text_vy: 'Оцени неделю' })
  assert.equal(same?.survey?.questions[0].text_vy, undefined)
  // Nothing to move, or question 1 already written.
  assert.equal(textToFirstQuestion({ ...step, text_ty: '', text_vy: '' }), null)
  const written = { ...step, survey: { ...step.survey, questions: [{ text: 'q', options: [] }] } }
  assert.equal(textToFirstQuestion(written), null)
})
