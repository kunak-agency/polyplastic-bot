// "Indicators" screen rules (api_contract.md, section "indicators", research
// 2026-10-24-overview-metrics.md). Every number, threshold and verdict comes
// from the server; this module only picks routes, labels and formats. Pure,
// types-only imports, so the node tests load it directly.
import type {
  FunnelLossReason,
  IndicatorPeriodKey,
  IndicatorTile,
  IndicatorTileKey,
  Indicators,
} from '@/types/api'

export const PERIODS: readonly IndicatorPeriodKey[] = ['30d', '90d', 'q']
export const DEFAULT_PERIOD: IndicatorPeriodKey = '30d'
// Server rule (services/indicators.go FunnelPercentMin), for the footnote only.
export const FUNNEL_PERCENT_MIN = 20

// ?period= of the route; anything else is the default period.
export function parsePeriod(v: unknown): IndicatorPeriodKey {
  return typeof v === 'string' && (PERIODS as readonly string[]).includes(v)
    ? (v as IndicatorPeriodKey)
    : DEFAULT_PERIOD
}

// A drill-down into a cabinet list with a preset filter, or none: only filters
// the registry and the queue already read (lib/newcomerFilters.ts, lib/tickets.ts).
export interface RouteTarget {
  name: 'newcomers' | 'tickets' | 'knowledge'
  query: Record<string, string>
}

export function tileTarget(key: IndicatorTileKey): RouteTarget | null {
  switch (key) {
    case 'connected_before_d0':
      return { name: 'newcomers', query: { chips: 'notConnected' } }
    case 'bot_closed':
      return { name: 'knowledge', query: { tab: 'unanswered' } }
    case 'first_response':
      return { name: 'tickets', query: {} }
    default:
      return null
  }
}

// "Lost: N (reason)" of the funnel: the registry has chips for "not connected"
// and "silent"; the other reasons have no filter, so no link.
export function lossTarget(reason: FunnelLossReason): RouteTarget | null {
  switch (reason) {
    case 'not_connected':
      return { name: 'newcomers', query: { chips: 'notConnected' } }
    case 'silent':
      return { name: 'newcomers', query: { chips: 'silent' } }
    default:
      return null
  }
}

// "Today" of the team: the HR lead's own screen when it is open to the viewer,
// otherwise the ticket queue (auth off has no "Today").
export function todayTarget(canDashboard: boolean): 'dashboard' | 'tickets' {
  return canDashboard ? 'dashboard' : 'tickets'
}

export function findTile(data: Indicators, key: IndicatorTileKey): IndicatorTile | undefined {
  return data.tiles.find((t) => t.key === key)
}

// Working minutes as hours and minutes: 95 → { h: 1, m: 35 }.
export function splitMinutes(min: number): { h: number; m: number } {
  const v = Math.max(0, Math.round(min))
  return { h: Math.floor(v / 60), m: v % 60 }
}

// The i18n key and params of the "versus the previous period" line, or null
// when there is nothing to say (the tile itself has no value).
export function vsPrev(t: IndicatorTile): { key: string; d: number } | null {
  if (t.status !== 'ok' || t.value === null) return null
  if (t.direction === null || t.delta === null) return { key: 'none', d: 0 }
  const d = Math.abs(t.delta)
  if (t.direction === 'same') return { key: 'same', d }
  if (t.unit === 'min') return { key: t.direction === 'up' ? 'upMin' : 'downMin', d }
  return { key: t.direction, d }
}

// A site column is "small" when every metric cell of it is hidden: the screen
// says "3 employees — indicators from 5" instead of a row of "hidden".
export function smallSites(data: Indicators): Set<string> {
  const out = new Set<string>()
  for (const s of data.sites.sites) {
    const cells = data.sites.rows
      .filter((r) => r.key !== 'cohort')
      .map((r) => r.cells.find((c) => c.site === s))
    if (cells.every((c) => !c || c.hidden)) out.add(s)
  }
  return out
}

// Display value of a site cell / average in its unit: "82" (percent, the
// template adds the sign), "1.4" (per 10), "61" (count).
export function unitValue(v: number, unit: string): string {
  return unit === 'per10' ? v.toFixed(1).replace('.', ',') : String(Math.round(v))
}
