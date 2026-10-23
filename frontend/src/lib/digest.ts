// Digest screen rules (api_contract.md v4): ISO weeks for the week switcher,
// signed deltas, pulse marks for the head-of-unit cards. Pure, types-only
// imports, so the node tests load it directly.
import type { DigestTheme, DigestTotals } from '@/types/api'

// "2026-W43" for a date (ISO-8601: the week with the year's first Thursday is 1).
export function isoWeek(date: Date): string {
  const d = new Date(Date.UTC(date.getFullYear(), date.getMonth(), date.getDate()))
  const day = d.getUTCDay() || 7
  d.setUTCDate(d.getUTCDate() + 4 - day)
  const yearStart = new Date(Date.UTC(d.getUTCFullYear(), 0, 1))
  const week = Math.ceil(((d.getTime() - yearStart.getTime()) / 86_400_000 + 1) / 7)
  return `${d.getUTCFullYear()}-W${String(week).padStart(2, '0')}`
}

// Monday of an ISO week as a local date; null for a malformed value.
export function weekMonday(week: string): Date | null {
  const m = /^(\d{4})-W(\d{2})$/.exec(week)
  if (!m) return null
  const year = Number(m[1])
  const w = Number(m[2])
  if (w < 1 || w > 53) return null
  const jan4 = new Date(year, 0, 4)
  const monday = new Date(year, 0, 4 - ((jan4.getDay() + 6) % 7) + 7 * (w - 1))
  return isoWeek(monday) === week ? monday : null
}

// The week n weeks away (negative — back).
export function shiftWeek(week: string, n: number): string {
  const mon = weekMonday(week)
  if (!mon) return week
  mon.setDate(mon.getDate() + 7 * n)
  return isoWeek(mon)
}

// "+4", "−3", "0"; null stays null (no comparison possible).
export function signed(n: number | null): string | null {
  if (n === null) return null
  if (n > 0) return `+${n}`
  if (n < 0) return `−${-n}`
  return '0'
}

// Difference of two nullable numbers.
export function delta(cur: number | null, prev: number | null): number | null {
  return cur === null || prev === null ? null : cur - prev
}

export type Trend = 'up' | 'down' | 'flat' | 'none'

// Direction of a change; for "bad" metrics (problems) the caller flips colour.
export function trendOf(n: number | null): Trend {
  if (n === null) return 'none'
  return n > 0 ? 'up' : n < 0 ? 'down' : 'flat'
}

export function totalsDelta(cur: DigestTotals, prev: DigestTotals) {
  return {
    on_pulse: cur.on_pulse - prev.on_pulse,
    response_rate_pct: delta(cur.response_rate_pct, prev.response_rate_pct),
    w_avg: delta(cur.w_avg, prev.w_avg),
    problems: cur.problems - prev.problems,
    critical: cur.critical - prev.critical,
  }
}

// Bar width of a theme relative to the largest one, in percent.
export function themeBars(themes: DigestTheme[]): number[] {
  const top = Math.max(1, ...themes.map((t) => t.newcomers))
  return themes.map((t) => Math.round((t.newcomers / top) * 100))
}

// Pulse mark for the head-of-unit card (ux-spec 3.1: "last pulse emoji"),
// as a level the template maps to a colour and an i18n word.
export type PulseLevel = 'good' | 'mid' | 'low' | 'none'
export function pulseLevel(v: number | null): PulseLevel {
  if (v === null) return 'none'
  if (v >= 8) return 'good'
  if (v >= 6) return 'mid'
  return 'low'
}
