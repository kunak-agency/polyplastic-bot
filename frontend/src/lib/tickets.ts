// Ticket queue rules (ux-spec 3.4, api_contract.md v2): SLA state, views,
// sorting and the visibility rules the cabinet applies on its side. Pure,
// types-only imports, so the node tests load it directly.
import type { Ticket, TicketCategory, TicketPriority, TicketStatus } from '@/types/api'

// ── Visibility (contract v2, "rules of code") ───────────────────────────────

// Seen only by the HR partner and security: the quote and the newcomer's words
// are hidden. F0/F4/F10 are NOT restricted (orchestrator's amendment, 24.10).
export const RESTRICTED_CATEGORIES: readonly TicketCategory[] = ['F5', 'F9']
// Never shown to the newcomer's manager (taxonomy manager_visibility), so
// "notify the manager" is not offered; the server answers PATCH with 422.
export const MANAGER_HIDDEN_CATEGORIES: readonly TicketCategory[] = ['F0', 'F4', 'F5', 'F9', 'F10']

// The server flag wins, the category is the fallback: a restricted ticket must
// not lose its banner because one response left the field out.
export function isRestricted(t: { category: TicketCategory; restricted?: boolean }): boolean {
  return t.restricted === true || RESTRICTED_CATEGORIES.includes(t.category)
}

// The role in the bot's signature of a reply (backend staffRole): security for
// the security route, the HR partner for F3/F4/F5 (taxonomy route hr_bp), the
// recruiter for the recruiter route, HR otherwise.
export type SignatureRole = 'hr' | 'hrPartner' | 'recruiter' | 'security'
const HR_PARTNER_CATEGORIES: readonly TicketCategory[] = ['F3', 'F4', 'F5']

export function signatureRole(t: Pick<Ticket, 'category' | 'route'>): SignatureRole {
  if (t.route === 'security') return 'security'
  if (HR_PARTNER_CATEGORIES.includes(t.category)) return 'hrPartner'
  if (t.route === 'recruiter') return 'recruiter'
  return 'hr'
}

export function canNotifyManager(category: TicketCategory): boolean {
  return !MANAGER_HIDDEN_CATEGORIES.includes(category)
}

// ── SLA ─────────────────────────────────────────────────────────────────────

// The deadline is the server's (working hours, bot time excluded); the cabinet
// only counts down to it. "soon" is the last hour.
export const SLA_SOON_MS = 60 * 60_000

export type SlaKind = 'none' | 'ok' | 'soon' | 'overdue'

export interface SlaState {
  kind: SlaKind
  // Time left (negative when overdue); 0 for "none".
  ms: number
}

export function slaState(t: Pick<Ticket, 'status' | 'sla_due_at'>, now: number): SlaState {
  if (t.status === 'resolved' || !t.sla_due_at) return { kind: 'none', ms: 0 }
  const due = Date.parse(t.sla_due_at)
  if (!Number.isFinite(due)) return { kind: 'none', ms: 0 }
  const ms = due - now
  if (ms < 0) return { kind: 'overdue', ms }
  return { kind: ms <= SLA_SOON_MS ? 'soon' : 'ok', ms }
}

export function isOverdue(t: Pick<Ticket, 'status' | 'sla_due_at'>, now: number): boolean {
  return slaState(t, now).kind === 'overdue'
}

export interface DurationParts {
  days: number
  hours: number
  minutes: number
}

// Absolute duration split for display; a started minute counts as a minute, so
// a timer never shows "0 min" while time is still left.
export function durationParts(ms: number): DurationParts {
  const total = Math.max(1, Math.ceil(Math.abs(ms) / 60_000))
  return {
    days: Math.floor(total / 1440),
    hours: Math.floor((total % 1440) / 60),
    minutes: total % 60,
  }
}

// ── Views (ux-spec 3.4, left rail) ──────────────────────────────────────────

// "Mine" needs auth and "Security" needs roles, neither exists in v2; the site
// view is the global site filter (stores/site.ts) sent as `site=`.
export const TICKET_VIEWS = ['all', 'unassigned', 'urgent', 'overdue', 'closed'] as const
export type TicketView = (typeof TICKET_VIEWS)[number]

export function isActive(t: Pick<Ticket, 'status'>): boolean {
  return t.status !== 'resolved'
}

export function matchesView(t: Ticket, view: TicketView, now: number): boolean {
  switch (view) {
    case 'all':
      return true
    case 'unassigned':
      return isActive(t) && !t.assignee_name
    case 'urgent':
      return isActive(t) && (t.priority === 'P0' || t.priority === 'P1')
    case 'overdue':
      return isOverdue(t, now)
    case 'closed':
      return t.status === 'resolved'
  }
}

export function applyView(list: Ticket[], view: TicketView, now: number): Ticket[] {
  return list.filter((t) => matchesView(t, view, now))
}

export function viewCounts(list: Ticket[], now: number): Record<TicketView, number> {
  const counts = Object.fromEntries(TICKET_VIEWS.map((v) => [v, 0])) as Record<TicketView, number>
  for (const t of list) for (const v of TICKET_VIEWS) if (matchesView(t, v, now)) counts[v]++
  return counts
}

export function activeCount(list: Pick<Ticket, 'status'>[]): number {
  return list.filter(isActive).length
}

// ── Sorting and the kanban ──────────────────────────────────────────────────

const PRIORITY_RANK: Record<TicketPriority, number> = { P0: 0, P1: 1, P2: 2 }

// Timestamps compared as instants: the server writes the site zone (+03:00), a
// string comparison would misorder it against UTC ones.
function at(iso: string): number {
  const ms = Date.parse(iso)
  return Number.isFinite(ms) ? ms : 0
}

function dueOf(t: Ticket): number {
  const due = t.sla_due_at ? Date.parse(t.sla_due_at) : NaN
  return Number.isFinite(due) ? due : Number.POSITIVE_INFINITY
}

// Active first: priority, then the nearest deadline (overdue ones are the
// nearest), then the oldest. Closed ones last, the latest closed first.
export function compareTickets(a: Ticket, b: Ticket): number {
  const ra = isActive(a) ? 0 : 1
  const rb = isActive(b) ? 0 : 1
  if (ra !== rb) return ra - rb
  if (ra === 1) return at(b.updated_at) - at(a.updated_at) || b.number - a.number
  return (
    PRIORITY_RANK[a.priority] - PRIORITY_RANK[b.priority] ||
    dueOf(a) - dueOf(b) ||
    at(a.created_at) - at(b.created_at) ||
    a.number - b.number
  )
}

export function sortTickets(list: Ticket[]): Ticket[] {
  return list.slice().sort(compareTickets)
}

export const KANBAN_COLUMNS: readonly TicketStatus[] = [
  'open',
  'assigned',
  'human_active',
  'resolved',
]

export function kanbanColumns(list: Ticket[]): Record<TicketStatus, Ticket[]> {
  const columns = Object.fromEntries(KANBAN_COLUMNS.map((s) => [s, [] as Ticket[]])) as Record<
    TicketStatus,
    Ticket[]
  >
  for (const t of sortTickets(list)) columns[t.status]?.push(t)
  return columns
}

// A newcomer's tickets for the trajectory page, the latest first.
export function ticketsOfNewcomer(list: Ticket[], newcomerId: string): Ticket[] {
  return list
    .filter((t) => t.newcomer_id === newcomerId)
    .sort((a, b) => at(b.created_at) - at(a.created_at))
}

// "Smirnov A." -> "SA": two letters for the assignee avatar.
export function initials(name: string | null): string {
  if (!name) return ''
  const words = name
    .replace(/[^\p{L}\s-]/gu, ' ')
    .split(/[\s-]+/)
    .filter(Boolean)
  return words
    .slice(0, 2)
    .map((w) => w[0].toUpperCase())
    .join('')
}
