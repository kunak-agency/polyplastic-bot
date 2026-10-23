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

export type EventStatus = 'scheduled' | 'sent' | 'answered' | 'skipped' | 'failed'

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
export type StepKind = 'text' | 'card' | 'file' | 'survey' | 'task'
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

export interface StepButton {
  label: string
  action: string
}

export interface SurveyQuestion {
  text: string
  options: string[]
}

export interface StepSurvey {
  scale: SurveyScale
  threshold: number | null
  questions: SurveyQuestion[]
}

export interface ScenarioStep {
  id: string
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
}

export interface ScenarioDetail extends Scenario {
  steps: ScenarioStep[]
}

export interface PublishRequest {
  comment: string
  apply: PublishApply
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
