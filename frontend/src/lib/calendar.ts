// Calendar arithmetic for the plan preview and the registry. Pure and free of
// relative imports (types only), so node --test loads it as is.
//
// Dates are contract dates, `YYYY-MM-DD` with no zone: they are computed in UTC
// so a local DST switch never moves a step by a day.
import type { SiteCode, StepKind } from '@/types/api'

const DAY_MS = 86_400_000

function toUtc(iso: string): number {
  const [y, m, d] = iso.split('-').map(Number)
  return Date.UTC(y, m - 1, d)
}

function fromUtc(ms: number): string {
  return new Date(ms).toISOString().slice(0, 10)
}

export function addDays(iso: string, days: number): string {
  return fromUtc(toUtc(iso) + days * DAY_MS)
}

// Whole calendar days from `from` to `to` (negative when `to` is earlier).
export function daysBetween(from: string, to: string): number {
  return Math.round((toUtc(to) - toUtc(from)) / DAY_MS)
}

// 0 = Sunday … 6 = Saturday, as Date#getDay.
export function weekday(iso: string): number {
  return new Date(toUtc(iso)).getUTCDay()
}

export function isWeekend(iso: string): boolean {
  const w = weekday(iso)
  return w === 0 || w === 6
}

// The local calendar date of `now` (the HR's machine), as the contract writes it.
export function localIsoDate(now: Date = new Date()): string {
  const y = now.getFullYear()
  const m = String(now.getMonth() + 1).padStart(2, '0')
  const d = String(now.getDate()).padStart(2, '0')
  return `${y}-${m}-${d}`
}

// Monday of the ISO week that holds `iso`.
export function weekStart(iso: string): string {
  return addDays(iso, -((weekday(iso) + 6) % 7))
}

// "D-3", "D0", "D+5" with a typographic minus: the label HR reads in the
// spec and in the customer's Excel. Not copy (no words), so not in the catalogue.
export function formatDayOffset(offset: number): string {
  if (offset === 0) return 'D0'
  return offset > 0 ? `D+${offset}` : `D−${Math.abs(offset)}`
}

// Working hours from the internal labour rules (ux-spec 2.3): Mon–Thu until
// 18:00, Friday until 17:00, never at weekends.
const FRIDAY_END = '17:00'

export interface FireDate {
  planned: string // start_date + day_offset
  date: string // after moving off non-working time
  shifted: boolean
}

// Where a step lands. Preview only: the backend scheduler is the authority and
// also knows holidays, which this does not. A weekend always moves the step to
// Monday; a Friday step past 17:00 moves too when it is "working hours only".
export function resolveFireDate(
  startDate: string,
  dayOffset: number,
  time: string,
  workHoursOnly: boolean,
): FireDate {
  const planned = addDays(startDate, dayOffset)
  let date = planned
  for (;;) {
    const w = weekday(date)
    if (w === 6) date = addDays(date, 2)
    else if (w === 0) date = addDays(date, 1)
    else if (w === 5 && workHoursOnly && time > FRIDAY_END) date = addDays(date, 3)
    else break
  }
  return { planned, date, shifted: date !== planned }
}

export interface PreviewStepInput {
  id: string
  day_offset: number
  time: string
  sites: SiteCode[]
  kind: StepKind
  title: string
  work_hours_only: boolean
}

export interface PreviewStep extends FireDate {
  id: string
  title: string
  kind: StepKind
  time: string
}

export interface PreviewDay {
  day_offset: number
  planned: string
  // Date the shifted steps of this day move to; null when none moves.
  movedTo: string | null
  steps: PreviewStep[]
}

export interface PlanPreview {
  days: PreviewDay[]
  total: number
  shiftedSteps: number
}

// A step with no sites applies to every site.
export function stepAppliesTo(step: { sites: SiteCode[] }, site: SiteCode | null): boolean {
  return !site || step.sites.length === 0 || step.sites.includes(site)
}

export function buildPlanPreview(
  steps: PreviewStepInput[],
  startDate: string,
  site: SiteCode | null,
): PlanPreview {
  const picked = steps
    .filter((s) => stepAppliesTo(s, site))
    .slice()
    .sort((a, b) => a.day_offset - b.day_offset || a.time.localeCompare(b.time))

  const days: PreviewDay[] = []
  let shiftedSteps = 0
  for (const s of picked) {
    const fire = resolveFireDate(startDate, s.day_offset, s.time, s.work_hours_only)
    if (fire.shifted) shiftedSteps++
    let day = days[days.length - 1]
    if (!day || day.day_offset !== s.day_offset) {
      day = { day_offset: s.day_offset, planned: fire.planned, movedTo: null, steps: [] }
      days.push(day)
    }
    if (fire.shifted && !day.movedTo) day.movedTo = fire.date
    day.steps.push({ id: s.id, title: s.title, kind: s.kind, time: s.time, ...fire })
  }
  return { days, total: picked.length, shiftedSteps }
}

// A contract date as a local Date at midnight, for display through $d: `new
// Date('2026-11-03')` is UTC midnight and shows the previous day west of UTC.
export function isoToLocalDate(iso: string): Date {
  const [y, m, d] = iso.split('-').map(Number)
  return new Date(y, m - 1, d)
}
