// Overview screen rules (ux-spec 3.1, api_contract.md v3): drill-down targets
// of the tiles and actions, the heat map matrix and the trend series. Pure,
// types-only imports, so the node tests load it directly.
import type {
  HeatCategoryCode,
  Overview,
  OverviewActionKind,
  OverviewHeatCell,
  OverviewRiskZone,
  OverviewStage,
  SiteCode,
} from '@/types/api'

// A drill-down: a route of the cabinet with a preset filter, or "filter the
// risk list on this page" (the registry has no "overdue" chip yet).
export type DrillTarget =
  | { kind: 'route'; name: 'newcomers' | 'tickets'; query: Record<string, string> }
  | { kind: 'overdue' }
  | { kind: 'none' }

export type TileKey = 'inAdaptation' | 'startingThisWeek' | 'overdue' | 'tickets' | 'pulse'

const newcomers = (query: Record<string, string> = {}): DrillTarget => ({
  kind: 'route',
  name: 'newcomers',
  query,
})
const tickets = (query: Record<string, string> = {}): DrillTarget => ({
  kind: 'route',
  name: 'tickets',
  query,
})

// Chips and views are the ones the registry and the queue already read from
// the query (lib/newcomerFilters.ts, lib/tickets.ts).
export const TILE_TARGETS: Record<TileKey, DrillTarget> = {
  inAdaptation: newcomers(),
  startingThisWeek: newcomers({ chips: 'startsThisWeek' }),
  overdue: { kind: 'overdue' },
  tickets: tickets(),
  pulse: { kind: 'none' },
}

export function actionTarget(kind: OverviewActionKind): DrillTarget {
  switch (kind) {
    case 'tickets_sla_overdue':
      return tickets({ view: 'overdue' })
    case 'tickets_unassigned':
      return tickets({ view: 'unassigned' })
    case 'tickets_sla_today':
      return tickets({ view: 'urgent' })
    case 'red_zones':
      return newcomers({ chips: 'red' })
    case 'not_connected_soon':
      return newcomers({ chips: 'notConnected' })
    case 'starting_today':
      return newcomers({ chips: 'startsThisWeek' })
    case 'surveys_overdue':
      return { kind: 'overdue' }
    case 'probation_decisions':
      return newcomers({ stage: 'probation' })
  }
}

// The registry reads `stage` once it supports it; until then the link opens
// the full registry, which is still the right place to look.
export function stageTarget(stage: OverviewStage): DrillTarget {
  return newcomers({ stage })
}

// Risk list: everything, or only those with something overdue.
export function filterRisks(items: OverviewRiskZone[], overdueOnly: boolean): OverviewRiskZone[] {
  return overdueOnly ? items.filter((r) => r.overdue) : items
}

// Total of the stage breakdown: equals tiles.in_adaptation by contract.
export function stagesTotal(stages: Overview['stages']): number {
  return stages.reduce((s, x) => s + x.count, 0)
}

// ── Heat map ───────────────────────────────────────────────────────────────

export interface HeatRow {
  code: HeatCategoryCode
  name: string
  cells: OverviewHeatCell[]
}

// Rows in the server's category order, columns in its site order; a missing
// cell reads as hidden (never as zero: zero is information, hidden is not).
export function heatRows(h: Overview['heatmap']): HeatRow[] {
  const byKey = new Map(h.cells.map((c) => [`${c.category}/${c.site}`, c]))
  return h.categories.map((cat) => ({
    code: cat.code,
    name: cat.name,
    cells: h.sites.map(
      (site) =>
        byKey.get(`${cat.code}/${site}`) ?? {
          category: cat.code,
          site,
          count: null,
          share: null,
          hidden: true,
        },
    ),
  }))
}

export function hiddenCount(h: Overview['heatmap']): number {
  return h.cells.filter((c) => c.hidden).length
}

// Sequential one-hue ramp (light to dark) for the share of the site's people.
export const HEAT_RAMP = ['#cde2fb', '#9ec5f4', '#6da7ec', '#3987e5', '#256abf', '#184f95']

export function heatColor(share: number | null): string {
  if (share === null || share <= 0) return '#f4f4f5'
  const i = Math.min(HEAT_RAMP.length - 1, Math.floor(share * HEAT_RAMP.length * 1.5))
  return HEAT_RAMP[i]
}

// ── Trend ──────────────────────────────────────────────────────────────────

// Fixed colour per entity (never by rank): a site keeps its colour whichever
// site the topbar filter leaves on screen. "All sites" is neutral ink.
export const SERIES_COLORS: Record<SiteCode | 'all', string> = {
  GrPP: '#2a78d6',
  Luzhniki: '#eb6834',
  GTP: '#1baf7a',
  all: '#52514e',
}

export interface TrendLine {
  site: SiteCode | 'all'
  values: (number | null)[]
  hidden: number
}

export function trendLines(t: Overview['trend']): TrendLine[] {
  return t.series.map((s) => ({
    site: s.site,
    values: t.weeks.map((w) => s.points.find((p) => p.week === w)?.avg ?? null),
    hidden: s.points.filter((p) => p.hidden).length,
  }))
}

// "D+12", "D0", "D−3" (the minus sign, not a hyphen) — the HR shorthand of ux-spec.
export function dayLabel(day: number): string {
  if (day === 0) return 'D0'
  return day > 0 ? `D+${day}` : `D−${-day}`
}
