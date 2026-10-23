// Step checks for the scenario table (ux-spec 3.5, "Status" column). Pure,
// types-only imports, so the node tests load it directly.
import type { ScenarioStep, Stage, StepKind, StepSurvey } from '@/types/api'

export const STEP_KINDS: readonly StepKind[] = ['text', 'card', 'file', 'survey', 'task']
export const STAGES: readonly Stage[] = ['before', 'day0', 'week1', 'month1', 'probation', 'done']

// The adaptation window of the case: three days before D0 to the end of probation.
export const FIRST_DAY = -3
export const LAST_DAY = 90

export type StepIssue =
  'outOfRange' | 'noTitle' | 'noText' | 'noSurvey' | 'surveyQuestions' | 'badTime'

const TIME_RE = /^([01]\d|2[0-3]):[0-5]\d$/

export function stepIssues(step: ScenarioStep): StepIssue[] {
  const issues: StepIssue[] = []
  if (step.day_offset < FIRST_DAY || step.day_offset > LAST_DAY) issues.push('outOfRange')
  if (!TIME_RE.test(step.time)) issues.push('badTime')
  if (!step.title.trim()) issues.push('noTitle')
  // Both address forms are required: the newcomer picks one at D-3, and a step
  // with only one text would go silent for the other half.
  if (!step.text_ty.trim() || !step.text_vy.trim()) issues.push('noText')
  if (step.kind === 'survey' && !step.survey) issues.push('noSurvey')
  // The server refuses the whole draft (422) over such a survey: flagged here
  // so HR sees which step to fix before saving.
  if (step.kind === 'survey' && step.survey && !surveyQuestionsOk(step.survey))
    issues.push('surveyQuestions')
  return issues
}

// The backend's rule (in/scenario.go): 1..20 questions, each with text, and
// at least two options per question of a "choice" survey.
function surveyQuestionsOk(survey: StepSurvey): boolean {
  const qs = survey.questions
  if (qs.length === 0 || qs.length > 20) return false
  return qs.every(
    (q) => q.text.trim() !== '' && (survey.scale !== 'choice' || q.options.length >= 2),
  )
}

// Survey settings for a step that became (or was imported as) a survey: one
// question taken from the step's own text, so the draft saves right away.
export function defaultSurvey(step: Pick<ScenarioStep, 'text_vy' | 'text_ty'>): StepSurvey {
  const text = (step.text_vy || step.text_ty).trim()
  return { scale: '1-10', threshold: 8, questions: text ? [{ text, options: [] }] : [] }
}

export function sortSteps(steps: ScenarioStep[]): ScenarioStep[] {
  return steps.slice().sort((a, b) => a.day_offset - b.day_offset || a.time.localeCompare(b.time))
}

// The stage a day falls into by default; HR can override it per step.
export function stageForDay(day: number): Stage {
  if (day < 0) return 'before'
  if (day === 0) return 'day0'
  if (day <= 7) return 'week1'
  if (day <= 30) return 'month1'
  if (day <= LAST_DAY) return 'probation'
  return 'done'
}

export function createStep(id: string, dayOffset: number): ScenarioStep {
  return {
    id,
    day_offset: dayOffset,
    time: '10:00',
    stage: stageForDay(dayOffset),
    sites: [],
    kind: 'text',
    title: '',
    text_ty: '',
    text_vy: '',
    buttons: [],
    survey: null,
    condition: null,
    work_hours_only: true,
  }
}
