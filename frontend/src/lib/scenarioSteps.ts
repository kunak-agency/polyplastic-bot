// Step checks for the scenario table (ux-spec 3.5, "Status" column). Pure,
// types-only imports, so the node tests load it directly.
import type {
  QuestionType,
  ScenarioStep,
  Stage,
  StepKind,
  StepSurvey,
  SurveyQuestion,
  SurveyScale,
} from '@/types/api'

export const STEP_KINDS: readonly StepKind[] = ['text', 'card', 'file', 'survey', 'task', 'input']
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
  // with only one text would go silent for the other half. A survey's text is
  // an optional intro before question 1: only a half-filled one is flagged.
  const ty = step.text_ty.trim() !== ''
  const vy = step.text_vy.trim() !== ''
  if (step.kind === 'survey' ? ty !== vy : !ty || !vy) issues.push('noText')
  if (step.kind === 'survey' && !step.survey) issues.push('noSurvey')
  // The server refuses the whole draft (422) over such a survey: flagged here
  // so HR sees which step to fix before saving.
  if (step.kind === 'survey' && step.survey && !surveyQuestionsOk(step.survey))
    issues.push('surveyQuestions')
  return issues
}

// The backend's rule (in/scenario.go): 1..20 questions, each with text, and
// at least two options per "choice" or "multi" question.
function surveyQuestionsOk(survey: StepSurvey): boolean {
  const qs = survey.questions
  if (qs.length === 0 || qs.length > 20) return false
  return qs.every((q) => {
    const type = questionType(q, survey.scale)
    return (
      q.text.trim() !== '' && ((type !== 'choice' && type !== 'multi') || q.options.length >= 2)
    )
  })
}

// ── Survey question types ───────────────────────────────────────────────────
// The type lives on the question. A question without one is the old format:
// it takes its type, scale and threshold from the survey (`survey.scale`) and
// keeps that meaning — the editor never rewrites it silently.

export function questionType(
  q: Pick<SurveyQuestion, 'type'>,
  surveyScale: SurveyScale,
): QuestionType {
  if (q.type) return q.type
  if (surveyScale === 'yesno') return 'yesno'
  if (surveyScale === 'choice') return 'choice'
  return 'scale'
}

// The comment a type asks for by default: a reason under the threshold on a
// scale, after "no" on yes/no, none otherwise.
export function defaultComment(type: QuestionType): SurveyQuestion['comment'] {
  if (type === 'scale') return 'below'
  if (type === 'yesno') return 'on_no'
  return ''
}

// Settings of a question switched to `type` (from the old format too): the
// scale and threshold it had are kept.
export function typedQuestion(
  q: SurveyQuestion,
  type: QuestionType,
  survey: Pick<StepSurvey, 'scale' | 'threshold'>,
): SurveyQuestion {
  const next: SurveyQuestion = { ...q, type, comment: defaultComment(type) }
  if (type === 'scale') {
    const inherited = survey.scale === '1-5' || survey.scale === '1-10' ? survey.scale : '1-10'
    next.scale = q.type ? (q.scale ?? '1-10') : inherited
    next.threshold = q.threshold ?? survey.threshold ?? 8
  } else {
    delete next.scale
    delete next.threshold
  }
  return next
}

// A new question always has its own type: the one of the question before it
// (with its scale, threshold and comment), a 1–10 scale for the first one.
export function newQuestion(survey: StepSurvey | null): SurveyQuestion {
  const blank: SurveyQuestion = { text: '', options: [] }
  const prev = survey?.questions[survey.questions.length - 1]
  if (!survey || !prev)
    return { ...blank, type: 'scale', scale: '1-10', threshold: 8, comment: 'below' }
  const type = questionType(prev, survey.scale)
  const q = typedQuestion(blank, type, survey)
  if (prev.type) {
    q.comment = prev.comment ?? ''
    if (type === 'scale') {
      q.scale = prev.scale ?? '1-10'
      q.threshold = prev.threshold ?? q.threshold
    }
  } else if (type === 'scale') q.threshold = prev.threshold ?? q.threshold
  return q
}

// Survey settings for a step that became a survey. The step's text stays its
// intro; question 1 starts empty (HR may move the text there on purpose).
export function defaultSurvey(): StepSurvey {
  return { scale: '1-10', threshold: 8, questions: [newQuestion(null)] }
}

// "Make the text question 1": the step's text becomes the first question and
// the intro is cleared, so the newcomer does not read it twice.
export function textToFirstQuestion(step: ScenarioStep): Partial<ScenarioStep> | null {
  const ty = step.text_ty.trim()
  const vy = step.text_vy.trim()
  const survey = step.survey
  if (!survey || (!ty && !vy)) return null
  const [first, ...rest] = survey.questions.length > 0 ? survey.questions : [newQuestion(survey)]
  if (first.text.trim()) return null
  const q: SurveyQuestion = { ...first, text: ty || vy }
  if (vy && vy !== q.text) q.text_vy = vy
  else delete q.text_vy
  return { text_ty: '', text_vy: '', survey: { ...survey, questions: [q, ...rest] } }
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

// ── Order of the steps inside a block ───────────────────────────────────────
// A block is the run of steps with one day and time (groupBlocks). The server
// stores the order of the list it is sent as `position` (PUT …/steps), so
// moving a step means moving it in the list; the list stays in plan order.

function sameBlock(
  a: Pick<ScenarioStep, 'day_offset' | 'time'>,
  b: Pick<ScenarioStep, 'day_offset' | 'time'>,
): boolean {
  return a.day_offset === b.day_offset && a.time === b.time
}

export function canMoveInBlock(steps: ScenarioStep[], id: string, by: -1 | 1): boolean {
  const i = steps.findIndex((s) => s.id === id)
  const j = i + by
  return i >= 0 && j >= 0 && j < steps.length && sameBlock(steps[i], steps[j])
}

// "Up" / "down": swaps the step with its neighbour in the block; at the edge
// of the block nothing moves.
export function moveInBlock(steps: ScenarioStep[], id: string, by: -1 | 1): ScenarioStep[] {
  if (!canMoveInBlock(steps, id, by)) return steps
  const i = steps.findIndex((s) => s.id === id)
  const out = steps.slice()
  ;[out[i], out[i + by]] = [out[i + by], out[i]]
  return out
}

// Drag and drop: the step takes the place of `targetId`. Only inside one
// block: another day or time is changed in the step itself.
export function moveToStep(steps: ScenarioStep[], id: string, targetId: string): ScenarioStep[] {
  const from = steps.findIndex((s) => s.id === id)
  const to = steps.findIndex((s) => s.id === targetId)
  if (from < 0 || to < 0 || from === to || !sameBlock(steps[from], steps[to])) return steps
  const out = steps.slice()
  const [step] = out.splice(from, 1)
  out.splice(to, 0, step)
  return out
}

// "Insert a message after": a new text step of the same block right after `id`.
export function insertAfter(
  steps: ScenarioStep[],
  id: string,
  newId: string,
): { steps: ScenarioStep[]; step: ScenarioStep } | null {
  const i = steps.findIndex((s) => s.id === id)
  if (i < 0) return null
  const src = steps[i]
  const step: ScenarioStep = {
    ...createStep(newId, src.day_offset),
    time: src.time,
    stage: src.stage,
    work_hours_only: src.work_hours_only,
  }
  const out = steps.slice()
  out.splice(i + 1, 0, step)
  return { steps: out, step }
}

export interface BlockMove {
  from: { day_offset: number; time: string; size: number } // size before the move
  to: { day_offset: number; time: string; size: number } // size after the move
  place: number // place of the step in its new block, from 1
}

// A new day or time takes the step out of its block: it goes to the end of the
// block of that day and time, or becomes a block of its own in plan order.
// `moved` tells HR about it when a block of several messages is involved.
export function relocateStep(
  steps: ScenarioStep[],
  id: string,
  patch: Partial<ScenarioStep>,
): { steps: ScenarioStep[]; moved: BlockMove | null } {
  const old = steps.find((s) => s.id === id)
  if (!old) return { steps, moved: null }
  const step = { ...old, ...patch }
  if (sameBlock(old, step))
    return { steps: steps.map((s) => (s.id === id ? step : s)), moved: null }
  const fromSize = steps.filter((s) => sameBlock(s, old)).length
  let out = steps.filter((s) => s.id !== id)
  let last = -1
  out.forEach((s, i) => {
    if (sameBlock(s, step)) last = i
  })
  if (last >= 0) out.splice(last + 1, 0, step)
  else out = sortSteps([...out, step])
  const block = out.filter((s) => sameBlock(s, step))
  const moved: BlockMove = {
    from: { day_offset: old.day_offset, time: old.time, size: fromSize },
    to: { day_offset: step.day_offset, time: step.time, size: block.length },
    place: block.findIndex((s) => s.id === id) + 1,
  }
  return { steps: out, moved: fromSize > 1 || block.length > 1 ? moved : null }
}

// The list as it is saved: `position` is the index (the server numbers it the
// same way; sent explicitly so the order is visible in the request).
export function withPositions(steps: ScenarioStep[]): ScenarioStep[] {
  return steps.map((s, i) => ({ ...s, position: i }))
}
