// Mirror of the backend contract (agency/system/api_contract.md): the single
// source of truth for response shapes. Screens and stores import types from here
// and invent nothing.
//
// Failures arrive as ApiError (see src/api/http.ts), NOT as AxiosError — the
// interceptor rejects a plain object, so `instanceof Error` misses it.
//
// Domain shapes (newcomers, tickets, scenarios) land below together with their
// PRD and the matching section of api_contract.md, one block per module, in the
// sidebar order.

export interface ApiError {
  status: number // 0 when the request never reached the server
  message: string // best-effort (may be axios' own text)
  serverMessage?: string // text taken from the response body
  code?: string // machine-readable code
  traceId?: string // backend trace id, for the log lookup
  details?: unknown
  // problem+json `errors[]`, accepted for tolerance: which field was rejected.
  fields?: FieldProblem[]
}

export interface FieldProblem {
  field: string
  code?: string
  message: string
}

// ── Health (GET /api/health) ────────────────────────────────────────────────

export type HealthState = 'ok' | 'down'

export interface HealthResponse {
  status: HealthState | (string & {})
  db: HealthState | (string & {})
  rag: HealthState | (string & {})
}

// ── Common (api_contract.md v1) ─────────────────────────────────────────────

// Lists come wrapped: { "items": [...], "total": N }.
export interface ListResponse<T> {
  items: T[]
  total: number
}

// Site codes are data, the same as stores/site.ts SITES.
export type SiteCode = 'GrPP' | 'Luzhniki' | 'GTP'

// ── Newcomers (GET/POST /api/newcomers, /api/newcomers/{id}) ────────────────

export type AddressForm = 'ty' | 'vy'
export type Consent = 'given' | 'declined'
export type Connection = 'invited' | 'connected' | 'stopped'
export type Stage = 'before' | 'day0' | 'week1' | 'month1' | 'probation' | 'done'
export type Risk = 'green' | 'yellow' | 'red'
export type Shift = '1' | '2' | '3'

export interface Newcomer {
  id: string
  full_name: string
  phone: string
  site: SiteCode
  department: string
  position: string
  manager_name: string
  mentor_name: string
  recruiter_name: string
  start_date: string // YYYY-MM-DD
  shift: Shift | null
  scenario_id: string
  scenario_version: number
  address_form: AddressForm | null
  consent: Consent | null
  connection: Connection
  day: number // days since D0, negative before the start
  stage: Stage
  invite_link: string
  last_activity_at: string | null
  last_pulse: number | null
  risk: Risk
  risk_reason: string | null
  open_tickets: number
}

// POST /api/newcomers: the card without computed fields. The scenario version is
// fixed by the server at launch time, so it is not sent.
export interface NewcomerCreate {
  full_name: string
  phone: string
  site: SiteCode
  department: string
  position: string
  manager_name: string
  mentor_name: string
  recruiter_name: string
  start_date: string
  shift: Shift | null
  scenario_id: string
}

// PATCH /api/newcomers/{id}: moves the start date; unsent events are re-planned
// by the server without duplicates (api_contract.md, backend note).
export interface NewcomerPatch {
  start_date: string
}

// POST /api/dev/simulate-start, APP_ENV=development only (api_contract.md,
// "dev"): plays the newcomer opening the invite and, optionally, answering the
// greeting and the consent.
export interface SimulateStartRequest {
  newcomer_id: string
  address_form?: AddressForm | null
  consent?: Consent | null
}

export interface NewcomerListQuery {
  site?: SiteCode | null
  q?: string
  risk?: Risk | null
}

// held — a step shown "on the recruiter's go-ahead" (flag_required) waits for the
// flag in the newcomer's card (contract v4).
export type EventStatus = 'scheduled' | 'sent' | 'answered' | 'skipped' | 'failed' | 'held'

export interface TimelineEvent {
  event_id: string
  step_id: string
  day_offset: number
  title: string
  kind: StepKind
  fire_at: string // ISO 8601 with zone
  status: EventStatus
  answer: string | null
  // What went to MAX, marked [TEST] by time travel; absent or null until sent.
  text?: string | null
}

// GET /timeline and POST /time-travel both answer { items } (no total).
export interface TimelineResponse {
  items: TimelineEvent[]
}

// ── Scenarios (GET /api/scenarios, /api/scenarios/{id}) ─────────────────────

export type ScenarioStatus = 'draft' | 'published'
// input — waits for the newcomer's own text and/or photo (06-excel-tz §2, r34).
export type StepKind = 'text' | 'card' | 'file' | 'survey' | 'task' | 'input'
export type SurveyScale = '1-10' | '1-5' | 'yesno' | 'choice'
export type PublishApply = 'new' | 'active'

export interface Scenario {
  id: string
  name: string
  sites: SiteCode[]
  version: number
  status: ScenarioStatus
  active_newcomers: number
  updated_at: string
}

// ── v4: branches, actions, media, per-question surveys (api_contract.md v4) ──

// A media library entry referenced by name: an import creates the name before
// HR uploads the file, so media_id may be absent.
export interface MediaRef {
  media_id?: string | null
  name: string
  // A file from the newcomer's own card instead of the library (r36).
  personal?: 'adaptation_plan' | null
}

export type ActionType =
  'escalate' | 'handoff' | 'set_field' | 'set_flag' | 'send_file' | 'open_free_text'
export type HandoffTo = 'recruiter' | 'hr' | 'security'
export type EscalateRoute = 'hr' | 'recruiter' | 'security'

export interface BranchAction {
  type: ActionType
  category?: string
  route?: EscalateRoute | ''
  notify_manager?: boolean
  to?: HandoffTo | ''
  field?: string
  value?: string
  flag?: string
  media?: string
  per_tag?: boolean
  prompt?: string
}

export type BranchThen = 'continue' | 'end_block' | 'goto_block'

export interface BranchMessage {
  text_ty: string
  text_vy: string
  media: MediaRef[]
  // Terminal: their own branch is ignored by the bot (depth is at most 2).
  buttons: StepButton[]
}

export interface Branch {
  messages: BranchMessage[] // at most 2
  actions: BranchAction[]
  then: BranchThen
  goto?: string
  // Shared branch: indices of the buttons (or survey options) it answers,
  // "if 1–2" (06-excel-tz §1.4). Absent — only the button that holds it.
  on?: number[]
}

export interface StepButton {
  label: string
  action: string
  branch?: Branch | null
  opens_free_text?: boolean
}

export interface NoAnswerRule {
  remind_after_h: number[]
  max: number
}

// How the flow moves past a step without a question: "button" — a "Next"
// button (the default when more steps follow in the block), "auto" — right
// away after a "typing…" pause. null — the default.
export type StepAdvance = 'button' | 'auto'

export type QuestionType = 'scale' | 'yesno' | 'choice' | 'multi' | 'text'

export type FlagSeverity = 'low' | 'medium' | 'high'

// A risk flag an option raises right away (06-excel-tz §1.3): F-code + severity.
export interface OptionFlag {
  code: string
  severity?: FlagSeverity
}

export interface SurveyQuestion {
  text: string
  options: string[]
  // The question and option labels for a newcomer on formal (vy) (text/options are
  // informal (ty), as in the customer's file). Empty — the informal (ty) text goes; options_vy
  // counts only when its length matches options, an empty label falls back.
  text_vy?: string
  options_vy?: string[]
  type?: QuestionType
  scale?: '1-10' | '1-5'
  threshold?: number | null
  anchors?: string[]
  // optional — a comment at any answer; below — a reason under the threshold;
  // on_no — a reason after "no".
  comment?: 'optional' | 'below' | 'on_no' | ''
  other?: boolean
  required?: boolean
  // Answer value as a string → its branch (text and actions before the next
  // question): choice — option number from 1, yesno — "1"/"0", scale — the number.
  branches?: Record<string, Branch>
  // Same keys → risk flag.
  option_flags?: Record<string, OptionFlag>
}

// Rules after a survey (r57): conditions on answers; rules of one group
// exclude each other ("OR"), groups are independent ("+").
export interface OutcomeCondition {
  q: number // question number, from 1
  in?: number[] // answer value is one of (option numbers from 1, yes/no 1/0, scale)
  contains?: number[] // a multi answer has any of these options (from 1)
}

export interface SurveyOutcome {
  if: OutcomeCondition[]
  match?: 'any' | 'all'
  group?: string
  messages: BranchMessage[]
  actions: BranchAction[]
}

export interface StepSurvey {
  scale: SurveyScale
  threshold: number | null
  questions: SurveyQuestion[]
  outcomes?: SurveyOutcome[]
}

export interface ScenarioStep {
  id: string
  // Order in the scenario (the order of the list in PUT …/steps): it is the
  // order of the messages inside a block of one day and time.
  position?: number
  day_offset: number
  time: string // HH:MM, local time of the site
  stage: Stage
  sites: SiteCode[]
  kind: StepKind
  title: string
  text_ty: string
  text_vy: string
  buttons: StepButton[]
  survey: StepSurvey | null
  condition: string | null
  work_hours_only: boolean
  // v4 (optional: older payloads and fixtures do not carry them)
  key?: string
  required?: boolean
  media?: MediaRef[]
  gallery?: boolean
  alt_text?: string | null
  hr_task?: boolean
  flag_required?: string | null
  no_answer?: NoAnswerRule | null
  advance?: StepAdvance | null
  // Texts rewritten by POST /scenarios/{id}/rewrite-vy; null — written by hand.
  ai_rewrite?: AIRewrite | null
}

// The mark of rewrite-vy on a step: what the generator wrote. The editor shows
// "AI, check it" while the step text still equals text_vy / text_ty here.
export interface AIRewrite {
  source: 'llm' | 'rule'
  text_vy?: string
  text_ty?: string
  fields: string[]
  at: string
}

// POST /scenarios/{id}/rewrite-vy (api_contract.md, v4).
export interface RewriteVyRequest {
  only_copied?: boolean
  neutralize?: boolean
  step_ids?: string[]
  scenario_texts?: boolean
}

export interface RewriteVyItem {
  step_id: string | null
  field: string
  before: string
  after: string
  source: 'llm' | 'rule'
}

export interface RewriteVyResponse {
  updated: number
  failed: number
  // failed by mode: the scenario-wide address_form_copied goes once failed_vy = 0
  failed_vy?: number
  failed_neutral?: number
  items: RewriteVyItem[]
  cleared_warnings: { step_id: string; code: string }[]
  scenario: ScenarioDetail
}

// Scenario commands (/food, /hr): the bot runs the actions on the command.
export interface ScenarioCommand {
  name: string
  text_ty?: string
  text_vy?: string
  actions: BranchAction[]
}

// Scenario-wide rules (r69, r70): reactions to events, not timeline steps.
// free_question — the refusal with a pair of buttons when the bot cannot
// answer; survey_reminder — a reminder about an unfinished survey.
export type ScenarioRuleKind = 'free_question' | 'survey_reminder'

export interface ScenarioRule {
  key: string
  kind: ScenarioRuleKind
  trigger: string
  text_ty: string
  text_vy: string
  media: MediaRef[]
  buttons: StepButton[]
  remind_after_h?: number[]
  row?: number
}

// PATCH /api/scenarios/{id}: scenario-wide commands and rules.
export interface ScenarioPatch {
  commands?: ScenarioCommand[]
  rules?: ScenarioRule[]
}

export interface ScenarioDetail extends Scenario {
  steps: ScenarioStep[]
  commands?: ScenarioCommand[]
  rules?: ScenarioRule[]
}

// POST /api/scenarios: a draft (version 0), empty or a copy of another
// scenario's last published steps under new step ids.
export interface ScenarioCreateRequest {
  name: string
  sites: SiteCode[]
  from_scenario_id: string | null
}

export interface PublishRequest {
  comment: string
  apply: PublishApply
}

// GET /api/scenarios/{id}/stats?site= — shares only at >= min_answers answers.
export interface ButtonStats {
  label: string
  picked: number
  share: number | null
  tickets: number
}

export interface StepStats {
  step_id: string
  sent: number
  answered: number
  no_answer: number
  median_answer_s: number | null
  buttons: ButtonStats[]
}

export interface ScenarioStats {
  site: SiteCode | null
  min_answers: number
  items: StepStats[]
}

// POST /api/scenarios/{id}/publish/preview — what publishing the saved draft does.
export type StepChangeKind = 'added' | 'removed' | 'changed'
export type StepChangeField =
  'when' | 'text' | 'sites' | 'buttons' | 'branches' | 'media' | 'survey' | 'settings'

export interface StepChange {
  step_id: string
  title: string
  day_offset: number
  time: string
  change: StepChangeKind
  fields: StepChangeField[]
  buttons_removed: string[]
}

export interface PublishPreview {
  next_version: number
  active_newcomers: number
  inside_survey: number
  removed_branches_live: number
  recommend: PublishApply
  changes: StepChange[]
}

// POST /api/scenarios/{id}/steps/{stepId}/send-test (development only): the
// draft step goes to a connected test newcomer marked [TEST]; with
// press_button — "walk this path", the button is pressed right away.
export interface SendTestRequest {
  newcomer_id: string
  press_button: number | null
}

// ── Tickets (api_contract.md v2, GET/POST /api/tickets…) ────────────────────

// F0..F10 are the red-flag codes of data/taxonomy.yaml; `question` is a plain
// "ask a human" with no flag.
export type TicketCategory =
  'F0' | 'F1' | 'F2' | 'F3' | 'F4' | 'F5' | 'F6' | 'F7' | 'F8' | 'F9' | 'F10' | 'question'
export type TicketPriority = 'P0' | 'P1' | 'P2'
export type TicketStatus = 'open' | 'assigned' | 'human_active' | 'resolved'
export type TicketRoute = 'hr' | 'recruiter' | 'manager' | 'security' | 'duty'

export interface Ticket {
  id: string
  number: number
  newcomer_id: string
  newcomer_name: string
  site: SiteCode
  day: number
  category: TicketCategory
  priority: TicketPriority
  status: TicketStatus
  route: TicketRoute
  assignee_name: string | null
  anonymous: boolean
  summary: string
  // null for F5/F9 (restricted) and whenever the server keeps no quote (F10).
  quote: string | null
  sla_due_at: string | null // ISO 8601 with zone
  created_at: string
  updated_at: string
  notify_manager: boolean
  // F5/F9 only: visible to the HR partner and security, the newcomer's words
  // are hidden from everyone in v2. F0/F4/F10 are hidden from the manager only
  // (orchestrator's amendment, 24.10): HR reads the thread and the quote.
  restricted: boolean
}

export type TicketMessageFrom = 'bot' | 'newcomer' | 'staff'

export interface TicketMessage {
  from: TicketMessageFrom
  author: string | null
  // null with hidden: true — the newcomer's words in a restricted ticket.
  text: string | null
  at: string
  internal: boolean
  hidden: boolean
}

export interface TicketDetail extends Ticket {
  messages: TicketMessage[]
}

export interface TicketListQuery {
  status?: TicketStatus | null
  site?: SiteCode | null
  route?: TicketRoute | null
  newcomer_id?: string | null
  mine?: boolean
}

export interface TicketAssignRequest {
  assignee_name: string
  comment: string
}

export interface TicketReplyRequest {
  text: string
  resolve: boolean
}

export interface TicketPatchRequest {
  notify_manager: boolean
}

export interface TicketNoteRequest {
  text: string
}

// ── v3 — knowledge base (api_contract.md, section v3) ───────────────────────

export type KnowledgeDocKind = 'pdf' | 'pptx' | 'xlsx' | 'text' | 'corpus'
export type KnowledgeDocSource = 'upload' | 'corpus'
export type KnowledgeDocStatus = 'ready' | 'needs_review' | 'not_indexed'

export interface KnowledgeDocument {
  // UUID of an uploaded document; "corpus:<rag_doc_id>" for the rag's own corpus.
  id: string
  rag_doc_id: string
  doc: string
  filename: string | null
  kind: KnowledgeDocKind
  source: KnowledgeDocSource
  // Empty — every site.
  sites: SiteCode[]
  version: number
  status: KnowledgeDocStatus
  // In the rag index right now (false while the rag is down).
  indexed: boolean
  chunks: number
  clauses: number
  low_confidence_pages: number[]
  size: number | null
  uploaded_by: string | null
  updated_at: string | null
  index_error: string | null
}

export interface KnowledgeDocumentList extends ListResponse<KnowledgeDocument> {
  rag: 'ok' | 'down'
}

export interface KnowledgeDocumentVersion {
  version: number
  current: boolean
  filename: string
  kind: KnowledgeDocKind
  size: number
  sha256: string
  chunks: number
  clauses: number
  low_confidence_pages: number[]
  uploaded_by: string | null
  uploaded_at: string
}

export interface KnowledgeUploadRequest {
  file: File
  doc?: string
  sites?: SiteCode[]
  uploaded_by?: string
}

export interface KnowledgeSource {
  doc: string
  clause_id: string
  page: number | null
  quote: string
}

export type KnowledgeAbstainReason = 'no_source' | 'below_threshold' | 'routed_topic'

export interface KnowledgeAskRequest {
  question: string
  doc?: string | null
  site?: SiteCode | null
}

export interface KnowledgeAskResponse {
  answer: string | null
  sources: KnowledgeSource[]
  confidence: 'high' | 'medium' | 'low'
  needs_escalation: boolean
  topic: string
  abstain_reason: KnowledgeAbstainReason | null
  // null — no document was named in the request.
  from_doc: boolean | null
}

export interface KnowledgeUnansweredCluster {
  key: string
  title: string
  keywords: string[]
  count: number
  newcomers: number
  reasons: Partial<Record<KnowledgeAbstainReason, number>>
  examples: string[]
  first_at: string
  last_at: string
}

export interface KnowledgeUnansweredResponse extends ListResponse<KnowledgeUnansweredCluster> {
  questions: number
  since: string
}

export interface KnowledgeUnansweredQuery {
  site?: SiteCode | null
  days?: number
  include_test?: boolean
}

// ── v3: overview (api_contract.md, section v3) ──────────────────────

export type OverviewStage = Exclude<Stage, 'done'>
export type RiskZoneLevel = 'red' | 'yellow'
export type OverviewActionKind =
  | 'tickets_sla_overdue'
  | 'tickets_unassigned'
  | 'red_zones'
  | 'tickets_sla_today'
  | 'not_connected_soon'
  | 'surveys_overdue'
  | 'starting_today'
  | 'probation_decisions'
export type HeatCategoryCode = 'F1' | 'F2' | 'F3' | 'F4' | 'F5' | 'F6' | 'F7' | 'F8' | 'F9' | 'F10'

export interface OverviewTiles {
  in_adaptation: number
  starting_this_week: number
  overdue: number
  tickets_open: number
  tickets_sla_overdue: number
  pulse_response: { percent: number | null; answered: number; sent: number; period_days: number }
}

export interface OverviewRiskZone {
  newcomer_id: string
  full_name: string
  site: SiteCode
  day: number
  stage: Stage
  zone: RiskZoneLevel
  reason: string
  reasons: string[]
  overdue: boolean
  last_pulse: number | null
  open_tickets: number
}

export interface OverviewHeatCell {
  category: HeatCategoryCode
  site: SiteCode
  // null when hidden: fewer than min_group people behind the cell.
  count: number | null
  share: number | null
  hidden: boolean
}

export interface OverviewTrendPoint {
  week: string
  avg: number | null
  respondents: number
  hidden: boolean
}

export interface Overview {
  site: SiteCode | null
  generated_at: string
  week: { from: string; to: string }
  tiles: OverviewTiles
  stages: { stage: OverviewStage; count: number }[]
  risk_zones: { items: OverviewRiskZone[]; total: number }
  action_today: { kind: OverviewActionKind; count: number }[]
  heatmap: {
    min_group: number
    period_days: number
    sites: SiteCode[]
    categories: { code: HeatCategoryCode; name: string }[]
    respondents: Partial<Record<SiteCode, number>>
    cells: OverviewHeatCell[]
  }
  trend: {
    weeks: string[]
    min_group: number
    series: { site: SiteCode | 'all'; points: OverviewTrendPoint[] }[]
  }
}

// ── v4: weekly digest and the head of unit (api_contract.md, "v4") ──────────

export interface DigestTotals {
  on_pulse: number
  answered: number
  response_rate_pct: number | null
  // Index W 0-100 by survey scales; null below min_group respondents.
  w_avg: number | null
  pulse_avg: number | null
  respondents: number
  problems: number
  critical: number
}

export interface DigestTheme {
  key: string
  label: string
  category: string | null
  newcomers: number
  mentions: number
  sites: SiteCode[]
  owner: string | null
  keywords: string[]
  examples: string[]
  prev_count: number | null
  prev_hidden: boolean
  delta: number | null
}

export interface DigestSiteRow {
  site: SiteCode
  on_pulse: number
  answered: number
  w_avg: number | null
  problems: number
  top_theme: string | null
}

export interface Digest {
  week: string
  from: string
  to: string
  site: SiteCode | null
  generated_at: string
  partial: boolean
  min_group: number
  totals: DigestTotals
  previous: DigestTotals
  themes: DigestTheme[]
  hidden_themes: number
  confidential: { present: boolean; count: number | null }
  risk_zones: { red: number; yellow: number; items: OverviewRiskZone[] }
  sites: DigestSiteRow[]
  probation: { count: number; this_week: boolean }
  text: {
    summary: string
    themes: string | null
    changes: string | null
    source: 'llm' | 'template'
    rejected: string[]
  }
  max_message: { text: string; length: number; link: string }
}

export interface DigestSendResult {
  week: string
  sent_to: number
  logged: boolean
  text: string
}

export type ManagerTaskKind = 'low_pulse_twice' | 'silent_3d' | 'probation_3d' | 'd30_meeting'

export interface ManagerTask {
  id: string
  kind: ManagerTaskKind
  status: 'open' | 'planned'
  text: string
  created_at: string
}

export interface ManagerCard {
  newcomer_id: string
  full_name: string
  site: SiteCode
  department: string
  position: string
  start_date: string
  day: number
  stage: Stage
  connection: string
  last_pulse: number | null
  probation_date: string
  attention: string[]
  meetings: ManagerTask[]
}

export interface ManagerView {
  manager_name: string
  connected: boolean
  items: ManagerCard[]
  total: number
}

export interface ManagerSummary {
  manager_name: string
  newcomers: number
  connected: boolean
}

export interface ManagerInvite {
  manager_name: string
  link: string
  expires_at: string
  connected: boolean
}

// ── v4: media library (api_contract.md v4, media library) ──

export type MediaKind = 'image' | 'file'
export type MediaStatus = 'missing' | 'ready'

// One file of an entry: site null is the common file, sent when the
// newcomer's site has none of its own.
export interface MediaFile {
  site: SiteCode | null
  filename: string
  mime: string
  size: number
  sha256: string
  updated_at: string
}

export interface MediaItem {
  id: string
  name: string
  kind: MediaKind
  files: MediaFile[]
  status: MediaStatus
  // Steps and button branches (latest published versions + drafts) that
  // reference the entry by id or by name.
  used_in: number
  // Former names: published versions keep referencing them.
  aliases: string[]
  updated_by: string | null
  updated_at: string
}

export interface MediaUploadRequest {
  file: File
  name?: string
  site?: SiteCode | null
  updated_by?: string
}

export interface MediaRenamed extends MediaItem {
  updated_refs: number
}

export interface MediaEnsureResponse extends ListResponse<MediaItem> {
  created: number
}
