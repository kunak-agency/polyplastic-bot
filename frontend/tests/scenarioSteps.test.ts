// Step checks of the scenario table (src/lib/scenarioSteps.ts).
import { test } from 'node:test'
import assert from 'node:assert/strict'
import {
  createStep,
  defaultSurvey,
  sortSteps,
  stageForDay,
  stepIssues,
} from '../src/lib/scenarioSteps.ts'

function filled(day: number) {
  return { ...createStep(`s${day}`, day), title: 't', text_ty: 'ty', text_vy: 'vy' }
}

test('a complete step inside the window has no issues', () => {
  assert.deepEqual(stepIssues(filled(0)), [])
  assert.deepEqual(stepIssues(filled(-3)), [])
  assert.deepEqual(stepIssues(filled(90)), [])
})

test('the window, the time, the texts and the survey are checked', () => {
  assert.deepEqual(stepIssues(filled(-4)), ['outOfRange'])
  assert.deepEqual(stepIssues({ ...filled(1), time: '25:00' }), ['badTime'])
  assert.deepEqual(stepIssues({ ...filled(1), text_vy: ' ' }), ['noText'])
  assert.deepEqual(stepIssues({ ...filled(1), kind: 'survey' }), ['noSurvey'])
  assert.deepEqual(stepIssues(createStep('x', 1)), ['noTitle', 'noText'])
})

test('a survey needs questions the server accepts', () => {
  const survey = (scale: '1-10' | 'choice', questions: { text: string; options: string[] }[]) => ({
    ...filled(3),
    kind: 'survey' as const,
    survey: { scale, threshold: 8, questions },
  })
  assert.deepEqual(stepIssues(survey('1-10', [])), ['surveyQuestions'])
  assert.deepEqual(stepIssues(survey('1-10', [{ text: ' ', options: [] }])), ['surveyQuestions'])
  assert.deepEqual(stepIssues(survey('choice', [{ text: 'q', options: ['a'] }])), [
    'surveyQuestions',
  ])
  assert.deepEqual(stepIssues(survey('1-10', [{ text: 'q', options: [] }])), [])
})

test('the default survey takes its question from the step text', () => {
  assert.deepEqual(defaultSurvey({ text_ty: 'ty', text_vy: ' vy ' }).questions, [
    { text: 'vy', options: [] },
  ])
  assert.deepEqual(defaultSurvey({ text_ty: '', text_vy: '' }).questions, [])
})

test('steps sort by day, then by time', () => {
  const steps = [
    { ...filled(0), id: 'b', time: '12:00' },
    { ...filled(0), id: 'a', time: '10:00' },
    { ...filled(-3), id: 'c' },
  ]
  assert.deepEqual(
    sortSteps(steps).map((s) => s.id),
    ['c', 'a', 'b'],
  )
})

test('default stage follows the day', () => {
  assert.equal(stageForDay(-3), 'before')
  assert.equal(stageForDay(0), 'day0')
  assert.equal(stageForDay(5), 'week1')
  assert.equal(stageForDay(30), 'month1')
  assert.equal(stageForDay(45), 'probation')
})
