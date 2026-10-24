// Branch model helpers for the step editor (api_contract.md v4; research
// verdict 2026-10-24 §Q1–Q3). Pure, types-only imports: node tests load it
// directly.
import type {
  Branch,
  BranchAction,
  ActionType,
  MediaRef,
  ScenarioStep,
  SiteCode,
  StepButton,
  StepSurvey,
  SurveyQuestion,
  SurveyScale,
} from '@/types/api'

// MAX has no label limit: the client itself cuts a long label on a narrow
// phone, so past LABEL_WARN characters HR only gets a warning. A row holds at
// most 7 buttons.
export const LABEL_WARN = 24
export const ROW_MAX = 7
export const BRANCH_MESSAGES_MAX = 2
// Three "send right away" steps in a row reach the newcomer as one burst.
export const AUTO_BURST = 3

// ── Blocks: steps with the same day and time ────────────────────────────────

export interface StepBlock {
  // Value a goto_block points at: the first step's key, else "day|time".
  key: string
  day_offset: number
  time: string
  steps: ScenarioStep[]
}

export function blockKeyOf(step: Pick<ScenarioStep, 'key' | 'day_offset' | 'time'>): string {
  return step.key?.trim() || `${step.day_offset}|${step.time}`
}

// Groups consecutive steps of a sorted list; the order inside a block is kept.
export function groupBlocks(steps: ScenarioStep[]): StepBlock[] {
  const out: StepBlock[] = []
  for (const s of steps) {
    const last = out[out.length - 1]
    if (last && last.day_offset === s.day_offset && last.time === s.time) {
      last.steps.push(s)
    } else {
      out.push({ key: blockKeyOf(s), day_offset: s.day_offset, time: s.time, steps: [s] })
    }
  }
  return out
}

// Every value a goto may name: block keys plus every step key (an import
// points at a block by the key of any of its rows).
export function gotoTargets(steps: ScenarioStep[]): Set<string> {
  const keys = new Set(groupBlocks(steps).map((b) => b.key))
  for (const s of steps) if (s.key?.trim()) keys.add(s.key.trim())
  return keys
}

// ── Buttons and branches ────────────────────────────────────────────────────

export function emptyBranch(): Branch {
  return {
    messages: [{ text_ty: '', text_vy: '', media: [], buttons: [] }],
    actions: [],
    then: 'continue',
  }
}

export function defaultAction(type: ActionType): BranchAction {
  switch (type) {
    case 'escalate':
      return { type, category: 'F2', route: 'hr', notify_manager: false }
    case 'handoff':
      return { type, to: 'recruiter' }
    case 'set_field':
      return { type, field: 'address_form', value: 'vy' }
    case 'set_flag':
      return { type, flag: '' }
    case 'send_file':
      return { type, media: '', per_tag: true }
    case 'open_free_text':
      return { type, prompt: '' }
  }
}

// The branch that answers button i: its own, or a shared one another button
// holds with i in `on` ("if 1–2", 06-excel-tz §1.4).
export function branchFor(
  step: Pick<ScenarioStep, 'buttons'>,
  i: number,
): { branch: Branch; owner: number } | null {
  const own = step.buttons[i]?.branch
  if (own) return { branch: own, owner: i }
  const owner = step.buttons.findIndex((b) => !!b.branch?.on?.includes(i))
  const shared = owner >= 0 ? step.buttons[owner].branch : null
  return shared ? { branch: shared, owner } : null
}

// "2/3": buttons answered by a branch / all buttons.
export function branchCount(step: Pick<ScenarioStep, 'buttons'>): {
  withBranch: number
  total: number
} {
  return {
    withBranch: step.buttons.filter((_, i) => branchFor(step, i) !== null).length,
    total: step.buttons.length,
  }
}

// A branch HR has opened but not filled: no text, no media, no actions, no
// terminal buttons and nothing else happens.
export function isBranchEmpty(b: Branch): boolean {
  const hasMessage = b.messages.some(
    (m) =>
      m.text_ty.trim() !== '' ||
      m.text_vy.trim() !== '' ||
      m.media.length > 0 ||
      m.buttons.length > 0,
  )
  return !hasMessage && b.actions.length === 0 && b.then === 'continue'
}

// Has the branch anything that deleting it would lose?
export function branchHasContent(b: Branch | null | undefined): boolean {
  return !!b && !isBranchEmpty(b)
}

export function labelLength(label: string): number {
  return [...label].length
}

// A label long enough that a phone may cut it (a warning, not an error).
export function labelIsLong(label: string): boolean {
  return labelLength(label) > LABEL_WARN
}

// ── "Next" between steps of a block ─────────────────────────────────────────

export type EffectiveAdvance = 'button' | 'auto' | 'none'

// A step that asks something (buttons or a survey) waits for the answer
// anyway; the last step of a block has nothing to move on to. Otherwise
// "Next" is the default (the person's decision), "auto" — only on request.
export function effectiveAdvance(step: ScenarioStep, hasNextInBlock: boolean): EffectiveAdvance {
  if (!hasNextInBlock || asksSomething(step)) return 'none'
  return step.advance === 'auto' ? 'auto' : 'button'
}

export function asksSomething(step: Pick<ScenarioStep, 'buttons' | 'kind'>): boolean {
  return step.buttons.length > 0 || step.kind === 'survey' || step.kind === 'input'
}

// What the "if no answer" field shows. With FEATURE_NUDGES_DEFAULT a question
// without its own no_answer is reminded by default (4 h / 24 h, holds the flow
// at most 48 h); "custom" is the step's own rule. Without the flag the old
// choice: silent or the step's own rule.
export type NoAnswerView = 'default' | 'custom' | 'silent'

export function noAnswerView(
  step: Pick<ScenarioStep, 'no_answer'>,
  nudgesDefault: boolean,
): NoAnswerView {
  if (step.no_answer) return 'custom'
  return nudgesDefault ? 'default' : 'silent'
}

// ── Validation (verdict §Q3) ────────────────────────────────────────────────

export type IssueLevel = 'error' | 'warning'

export type IssueCode =
  // errors: block publishing
  | 'labelEmpty'
  | 'tooManyButtons'
  | 'handoffNoTarget'
  | 'gotoNowhere'
  // warnings: shown, publishing allowed
  | 'labelTooLong'
  | 'mediaMissing'
  | 'siteVariantMissing'
  | 'branchEmpty'
  | 'autoBurst'

export interface Issue {
  level: IssueLevel
  code: IssueCode
  // Index of the step button the issue sits in (its branch tab), if any.
  button?: number
  // Survey question (from 0) whose option branch or outcome holds the issue.
  question?: number
  // Free parameter for the message: a label, a media name, a site list.
  detail?: string
}

export const ERROR_CODES: readonly IssueCode[] = [
  'labelEmpty',
  'tooManyButtons',
  'handoffNoTarget',
  'gotoNowhere',
]

function issue(code: IssueCode, button?: number, detail?: string): Issue {
  return {
    level: ERROR_CODES.includes(code) ? 'error' : 'warning',
    code,
    ...(button === undefined ? {} : { button }),
    ...(detail === undefined ? {} : { detail }),
  }
}

export interface ValidateContext {
  gotoTargets: Set<string>
  // Media status by name from the media library; absent — only media_id counts.
  mediaStatus?: Map<string, 'missing' | 'ready'>
}

function mediaMissing(ref: MediaRef, ctx: ValidateContext): boolean {
  if (ref.personal) return false
  const st = ctx.mediaStatus?.get(ref.name)
  if (st) return st === 'missing'
  return !ref.media_id
}

function checkButtons(buttons: StepButton[], out: Issue[], at?: number): void {
  if (buttons.length > ROW_MAX) out.push(issue('tooManyButtons', at, String(buttons.length)))
  buttons.forEach((b, i) => {
    const where = at ?? i
    if (!b.label.trim()) out.push(issue('labelEmpty', where))
    else if (labelIsLong(b.label)) out.push(issue('labelTooLong', where, b.label))
  })
}

function checkMedia(refs: MediaRef[] | undefined, ctx: ValidateContext, out: Issue[], at?: number) {
  for (const r of refs ?? []) if (mediaMissing(r, ctx)) out.push(issue('mediaMissing', at, r.name))
}

function checkBranch(b: Branch, i: number, ctx: ValidateContext, out: Issue[]): void {
  if (isBranchEmpty(b)) out.push(issue('branchEmpty', i))
  for (const m of b.messages) {
    checkMedia(m.media, ctx, out, i)
    checkButtons(m.buttons, out, i)
  }
  for (const a of b.actions) {
    if (a.type === 'handoff' && !a.to) out.push(issue('handoffNoTarget', i))
    if (a.type === 'send_file') {
      if (!a.media?.trim()) out.push(issue('mediaMissing', i, ''))
      else if (ctx.mediaStatus?.get(a.media) === 'missing')
        out.push(issue('mediaMissing', i, a.media))
    }
  }
  if (b.then === 'goto_block' && !(b.goto && ctx.gotoTargets.has(b.goto)))
    out.push(issue('gotoNowhere', i, b.goto ?? ''))
}

// Issues of one step on its own (block-level ones come from validateScenario).
export function validateStep(step: ScenarioStep, ctx: ValidateContext): Issue[] {
  const out: Issue[] = []
  checkButtons(step.buttons, out)
  checkMedia(step.media, ctx, out)
  step.buttons.forEach((b, i) => {
    if (b.branch) checkBranch(b.branch, i, ctx, out)
  })
  // Option branches of a survey and the rules after it run the same actions.
  step.survey?.questions.forEach((q, qi) => {
    for (const br of Object.values(q.branches ?? {})) {
      const own: Issue[] = []
      checkBranch(br, -1, ctx, own)
      for (const x of own) out.push({ ...x, button: undefined, question: qi })
    }
  })
  for (const o of step.survey?.outcomes ?? []) {
    const own: Issue[] = []
    checkBranch({ messages: o.messages, actions: o.actions, then: 'continue' }, -1, ctx, own)
    for (const x of own) if (x.code !== 'branchEmpty') out.push({ ...x, button: undefined })
  }
  return out
}

// All issues by step id: per-step checks plus the block ones — a missing site
// variant and a burst of "auto" steps.
export function validateScenario(
  steps: ScenarioStep[],
  scenarioSites: SiteCode[],
  allSites: readonly SiteCode[],
  ctx: Omit<ValidateContext, 'gotoTargets'> = {},
): Map<string, Issue[]> {
  const full: ValidateContext = { ...ctx, gotoTargets: gotoTargets(steps) }
  const out = new Map<string, Issue[]>(steps.map((s) => [s.id, validateStep(s, full)]))
  const sites = scenarioSites.length > 0 ? scenarioSites : [...allSites]
  for (const block of groupBlocks(steps)) {
    // Two or more site-specific steps in a block are variants of one message
    // (verdict §1: r9/10, r26–28): a site of the scenario with none is a gap.
    const variants = block.steps.filter((s) => s.sites.length > 0)
    if (variants.length >= 2) {
      const covered = new Set(variants.flatMap((s) => s.sites))
      const missing = sites.filter((c) => !covered.has(c))
      if (missing.length > 0)
        for (const s of variants)
          out.get(s.id)?.push(issue('siteVariantMissing', undefined, missing.join(', ')))
    }
    let run: ScenarioStep[] = []
    const flush = () => {
      if (run.length >= AUTO_BURST)
        for (const s of run) out.get(s.id)?.push(issue('autoBurst', undefined, String(run.length)))
      run = []
    }
    for (const s of block.steps) {
      if (s.advance === 'auto' && !asksSomething(s)) run.push(s)
      else flush()
    }
    flush()
  }
  return out
}

export function hasErrors(issues: Iterable<Issue[]>): boolean {
  for (const list of issues) if (list.some((i) => i.level === 'error')) return true
  return false
}

export function countIssues(issues: Iterable<Issue[]>): { errors: number; warnings: number } {
  let errors = 0
  let warnings = 0
  for (const list of issues)
    for (const i of list) {
      if (i.level === 'error') errors++
      else warnings++
    }
  return { errors, warnings }
}

// Button statistics badge (verdict §Q7): a share only at >= minAnswers answers.
export function statsBadge(
  b: { share: number | null; picked: number } | undefined,
): { percent: number; picked: number } | null {
  if (!b || b.share === null) return null
  return { percent: Math.round(b.share * 100), picked: b.picked }
}

// ── Survey options ──────────────────────────────────────────────────────────

// Answer values of a question as the bot records them — the keys of
// `branches` and `option_flags`: choice/multi — option number from 1, yes/no —
// "1"/"0", scale — each number. `label` is null for yes/no (the screen names it).
export function optionKeys(
  q: Pick<SurveyQuestion, 'type' | 'options' | 'scale'>,
  surveyScale: SurveyScale,
): { key: string; label: string | null }[] {
  const type =
    q.type ?? (surveyScale === 'yesno' ? 'yesno' : surveyScale === 'choice' ? 'choice' : 'scale')
  if (type === 'choice' || type === 'multi')
    return q.options.map((o, i) => ({ key: String(i + 1), label: o }))
  if (type === 'yesno')
    return [
      { key: '1', label: null },
      { key: '0', label: null },
    ]
  if (type === 'scale') {
    const max = (q.type ? (q.scale ?? '1-10') : surveyScale) === '1-5' ? 5 : 10
    return Array.from({ length: max }, (_, i) => ({ key: String(i + 1), label: String(i + 1) }))
  }
  return []
}

// ── Editing survey options ──────────────────────────────────────────────────

// Do the options of this question name answer keys? choice/multi — yes (option
// number from 1); a scale lists reason buttons, which are not keys.
export function optionsAreKeys(q: Pick<SurveyQuestion, 'type'>, surveyScale: SurveyScale): boolean {
  const type =
    q.type ?? (surveyScale === 'yesno' ? 'yesno' : surveyScale === 'choice' ? 'choice' : 'scale')
  return type === 'choice' || type === 'multi'
}

// Rebuilds a question for a new option list: `order[n]` is the old index of
// the option now at n, or -1 for a new one. Branches and flags keyed by the
// option number follow their option; those of removed options go. Rules after
// the survey that name the option (`in`, `contains` of question `qNumber`)
// follow it too.
export function reorderOptions(
  survey: StepSurvey,
  qi: number,
  options: string[],
  order: number[],
): StepSurvey {
  const q = survey.questions[qi]
  if (!q) return survey
  const keyed = optionsAreKeys(q, survey.scale)
  const next: SurveyQuestion = { ...q, options }
  // The formal (vy) labels follow their options (new index → old index; a new option gets none).
  if (q.options_vy)
    next.options_vy = order.map((old) => (old >= 0 ? (q.options_vy?.[old] ?? '') : ''))
  if (!keyed) return { ...survey, questions: survey.questions.map((x, j) => (j === qi ? next : x)) }

  // Old option number (from 1) → new one; removed options are absent.
  const moved = new Map<number, number>()
  order.forEach((old, n) => {
    if (old >= 0) moved.set(old + 1, n + 1)
  })
  const remapKeys = <T>(rec: Record<string, T> | undefined): Record<string, T> | undefined => {
    if (!rec) return undefined
    const out: Record<string, T> = {}
    for (const [k, v] of Object.entries(rec)) {
      const to = moved.get(Number(k))
      if (to !== undefined) out[String(to)] = v
    }
    return Object.keys(out).length > 0 ? out : undefined
  }
  next.branches = remapKeys(q.branches)
  next.option_flags = remapKeys(q.option_flags)
  if (!next.branches) delete next.branches
  if (!next.option_flags) delete next.option_flags

  const qNumber = qi + 1
  const remapList = (list: number[] | undefined): number[] | undefined =>
    list?.flatMap((n) => {
      const to = moved.get(n)
      return to === undefined ? [] : [to]
    })
  const outcomes = survey.outcomes?.map((o) => ({
    ...o,
    if: o.if.map((c) => {
      if (c.q !== qNumber) return c
      const r = { ...c }
      if (c.in) r.in = remapList(c.in)
      if (c.contains) r.contains = remapList(c.contains)
      return r
    }),
  }))
  return {
    ...survey,
    questions: survey.questions.map((x, j) => (j === qi ? next : x)),
    ...(outcomes ? { outcomes } : {}),
  }
}
