// Role dashboards described by config (research 2026-10-24-role-dashboards.md,
// api_contract_auth.md). The layout reads only these tables: a new tile is a
// row here plus its key in the server's `tiles`; a new "Today" button is a row
// in TODAY_ACTIONS. Pure, types-only imports: the node tests load it directly.
import type { DashTile, DashboardItem, PulseWeek, TodayActionId, TodayRow, Zone } from '@/types/api'

// ── Tiles ───────────────────────────────────────────────────────────────────

export interface TileConfig {
  // Key in the server's `tiles`, also the `?tile=` value of the page.
  id: string
  // i18n keys: dashboard.tiles.<id>.label / .hint (.hint gets {n} = secondary).
  labelKey: string
  hintKey?: string
  // 'pair' shows "value / secondary"; 'count' the value alone.
  format: 'count' | 'pair'
  // A tile without `ids` from the server links somewhere instead of filtering.
  to?: { name: string; query?: Record<string, string> }
}

function tile(id: string, format: TileConfig['format'] = 'count', to?: TileConfig['to']) {
  return {
    id,
    labelKey: `dashboard.tiles.${id}.label`,
    hintKey: `dashboard.tiles.${id}.hint`,
    format,
    to,
  }
}

// Recruiter: 6 filter tiles (research §1).
export const RECRUITER_TILES: readonly TileConfig[] = [
  tile('not_answering'),
  tile('sla', 'pair'),
  tile('starting', 'pair'),
  tile('ip_decisions'),
  tile('low_pulse'),
  tile('catching_up'),
]

// HR lead: 6 tiles (research §2); they lead to the matching list.
export const HR_LEAD_TILES: readonly TileConfig[] = [
  tile('unassigned_tickets', 'count', { name: 'tickets', query: { view: 'unassigned' } }),
  tile('sla_overdue', 'count', { name: 'tickets', query: { view: 'overdue' } }),
  tile('no_recruiter', 'count', { name: 'newcomers', query: { recruiter: 'none' } }),
  tile('red_zone', 'count', { name: 'newcomers', query: { chips: 'red' } }),
  tile('pulse_response', 'pair'),
  tile('ip_week', 'count'),
]

export function tileById(
  tiles: readonly TileConfig[],
  id: string | null | undefined,
): TileConfig | null {
  return tiles.find((t) => t.id === id) ?? null
}

export function tileOf(map: Record<string, DashTile>, cfg: TileConfig): DashTile {
  return map[cfg.id] ?? { value: 0, red: false }
}

export function tileText(t: DashTile, cfg: TileConfig): string {
  return cfg.format === 'pair' ? `${t.value} / ${t.secondary ?? 0}` : String(t.value)
}

// Ids under the picked tile; null — no tile picked (or it has no ids).
export function tileIds(map: Record<string, DashTile>, cfg: TileConfig | null): Set<string> | null {
  if (!cfg) return null
  const ids = map[cfg.id]?.ids
  return ids ? new Set(ids) : null
}

// ── "Today" ─────────────────────────────────────────────────────────────────

export const TODAY_LIMIT = 10

// Red above yellow, then by deadline, then the furthest along (research §1).
// The server sorts too; this keeps the order if it does not.
export function sortToday(rows: readonly TodayRow[]): TodayRow[] {
  const rank: Record<Zone, number> = { red: 0, yellow: 1 }
  const due = (r: TodayRow) => (r.due_at ? Date.parse(r.due_at) : Number.POSITIVE_INFINITY)
  return rows
    .slice()
    .sort(
      (a, b) => rank[a.zone] - rank[b.zone] || due(a) - due(b) || (b.day ?? -999) - (a.day ?? -999),
    )
    .slice(0, TODAY_LIMIT)
}

// What a button does: `go` — a route, `call` — an API action of the page.
export type ActionTarget =
  | {
      kind: 'go'
      to: { name: string; params?: Record<string, string>; query?: Record<string, string> }
    }
  | { kind: 'call'; call: 'remind' | 'take' }

export interface TodayActionConfig {
  labelKey: string
  primary?: boolean
  target: (row: TodayRow) => ActionTarget | null
}

const card = (row: TodayRow, query?: Record<string, string>): ActionTarget | null =>
  row.newcomer_id
    ? { kind: 'go', to: { name: 'newcomer', params: { id: row.newcomer_id }, query } }
    : null
const ticket = (row: TodayRow): ActionTarget | null =>
  row.ticket_id ? { kind: 'go', to: { name: 'ticket', params: { id: row.ticket_id } } } : null

export const TODAY_ACTIONS: Record<TodayActionId, TodayActionConfig> = {
  // The card opens with the "Mark: contacted" panel.
  contact: {
    labelKey: 'dashboard.actions.contact',
    primary: true,
    target: (r) => card(r, { contact: '1' }),
  },
  remind: {
    labelKey: 'dashboard.actions.remind',
    target: (r) => (r.newcomer_id ? { kind: 'call', call: 'remind' } : null),
  },
  take: {
    labelKey: 'dashboard.actions.take',
    primary: true,
    target: (r) => (r.ticket_id ? { kind: 'call', call: 'take' } : null),
  },
  open: { labelKey: 'dashboard.actions.open', target: (r) => ticket(r) ?? card(r) },
  // hr_lead: the ticket page has the assign dialog.
  assign: { labelKey: 'dashboard.actions.assign', primary: true, target: ticket },
  // The ticket page has the assign dialog; a newcomer's card the recruiter picker.
  reassign: { labelKey: 'dashboard.actions.reassign', target: (r) => ticket(r) ?? card(r) },
  pin: {
    labelKey: 'dashboard.actions.pin',
    primary: true,
    target: (r) =>
      card(r) ?? { kind: 'go', to: { name: 'newcomers', query: { recruiter: 'none' } } },
  },
  owner: {
    labelKey: 'dashboard.actions.owner',
    target: () => ({ kind: 'go', to: { name: 'analytics' } }),
  },
}

// ── Chips of the "mine" list ────────────────────────────────────────────────

export interface Chip {
  key: string
  params: Record<string, string | number>
  tone: 'danger' | 'warning' | 'info' | 'muted'
}

export function chipsFor(item: DashboardItem | undefined): Chip[] {
  if (!item) return []
  const out: Chip[] = []
  if (item.silent_days !== null && item.silent_days > 0)
    out.push({
      key: 'silent',
      params: { n: item.silent_days },
      tone: item.zone === 'red' ? 'danger' : 'warning',
    })
  if (item.catchup)
    out.push({
      key: 'catchup',
      params: { done: item.catchup.done, total: item.catchup.total },
      tone: 'info',
    })
  if (item.tickets_sla_overdue > 0)
    out.push({ key: 'slaOverdue', params: { n: item.tickets_sla_overdue }, tone: 'danger' })
  else if (item.tickets_open > 0)
    out.push({ key: 'tickets', params: { n: item.tickets_open }, tone: 'muted' })
  if (item.ip_decision_date)
    out.push({ key: 'ipDecision', params: { date: item.ip_decision_date }, tone: 'info' })
  return out
}

// ── Small numbers (research §6) ─────────────────────────────────────────────

export const MIN_GROUP = 5
export const PERCENT_FROM = 20

// "12 of 15" under 20, "80 %" from 20, null under 5 ("too little data").
export function share(
  part: number,
  total: number,
):
  | { kind: 'few' }
  | { kind: 'count'; part: number; total: number }
  | { kind: 'percent'; value: number } {
  if (total < MIN_GROUP) return { kind: 'few' }
  if (total < PERCENT_FROM) return { kind: 'count', part, total }
  return { kind: 'percent', value: Math.round((part / total) * 100) }
}

export function toneShown(p: PulseWeek): boolean {
  return !!p.tone && p.tone.n >= MIN_GROUP
}

// The empty state: nothing to do today and every tile at zero.
export function isQuiet(today: readonly TodayRow[], tiles: Record<string, DashTile>): boolean {
  return today.length === 0 && Object.values(tiles).every((t) => t.value === 0)
}

// Load table: overdue first, then open tickets; no ranking by speed.
export function sortLoad<
  T extends { tickets_sla_overdue: number; tickets_open: number; full_name: string },
>(rows: readonly T[]): T[] {
  return rows
    .slice()
    .sort(
      (a, b) =>
        b.tickets_sla_overdue - a.tickets_sla_overdue ||
        b.tickets_open - a.tickets_open ||
        a.full_name.localeCompare(b.full_name),
    )
}

// Users table of Settings (hr_lead): `key` — field, `labelKey` — team.columns.<key>.
export interface TeamColumn {
  key: 'newcomers' | 'tickets_open' | 'tickets_sla_overdue'
  labelKey: string
  warn?: boolean
}

export const TEAM_COLUMNS: readonly TeamColumn[] = [
  { key: 'newcomers', labelKey: 'team.columns.newcomers' },
  { key: 'tickets_open', labelKey: 'team.columns.ticketsOpen' },
  { key: 'tickets_sla_overdue', labelKey: 'team.columns.slaOverdue', warn: true },
]
