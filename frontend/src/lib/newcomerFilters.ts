// Registry chips and sorting (ux-spec 3.2). Pure, types-only imports, so the
// node tests load it directly. The site and the search go to the server
// (`site=`, `q=`); chips are computed here from the fields the list returns.
import type { Newcomer, Risk } from '@/types/api'

// Chips without a source in contract v1 are left out: "Mine" needs auth and
// "Overdue" needs task data, neither exists yet.
export const NEWCOMER_CHIPS = ['startsThisWeek', 'silent', 'red', 'notConnected'] as const
export type NewcomerChip = (typeof NEWCOMER_CHIPS)[number]

// "Did not answer the bot for more than 3 days".
export const SILENT_AFTER_DAYS = 3

const DAY_MS = 86_400_000

function isoDate(d: Date): string {
  const y = d.getFullYear()
  const m = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  return `${y}-${m}-${day}`
}

// Monday..Sunday of the local week holding `now`, as YYYY-MM-DD.
function weekBounds(now: Date): [string, string] {
  const monday = new Date(now.getFullYear(), now.getMonth(), now.getDate())
  monday.setDate(monday.getDate() - ((monday.getDay() + 6) % 7))
  const sunday = new Date(monday)
  sunday.setDate(monday.getDate() + 6)
  return [isoDate(monday), isoDate(sunday)]
}

export function matchesChip(n: Newcomer, chip: NewcomerChip, now: Date): boolean {
  switch (chip) {
    case 'startsThisWeek': {
      const [from, to] = weekBounds(now)
      return n.start_date >= from && n.start_date <= to
    }
    case 'silent': {
      // Only someone who is in the dialog can be silent; "not connected" is its
      // own chip, and "stopped the bot" is a status, not silence.
      if (n.connection !== 'connected') return false
      if (!n.last_activity_at) return true
      const last = Date.parse(n.last_activity_at)
      return Number.isFinite(last) && now.getTime() - last > SILENT_AFTER_DAYS * DAY_MS
    }
    case 'red':
      return n.risk === 'red'
    case 'notConnected':
      return n.connection === 'invited'
  }
}

// Chips narrow together (AND): "red" + "this week" is the red ones of this week.
export function applyChips(
  list: Newcomer[],
  chips: readonly NewcomerChip[],
  now: Date,
): Newcomer[] {
  if (chips.length === 0) return list
  return list.filter((n) => chips.every((c) => matchesChip(n, c, now)))
}

export function chipCounts(list: Newcomer[], now: Date): Record<NewcomerChip, number> {
  const counts = Object.fromEntries(NEWCOMER_CHIPS.map((c) => [c, 0])) as Record<
    NewcomerChip,
    number
  >
  for (const n of list) for (const c of NEWCOMER_CHIPS) if (matchesChip(n, c, now)) counts[c]++
  return counts
}

export type NewcomerSort = 'new' | 'risk' | 'day'

const RISK_RANK: Record<Risk, number> = { red: 0, yellow: 1, green: 2 }

// "new": the latest added first (ids are UUIDv7, ordered by creation time);
// "risk": red first, then the furthest along; "day": the furthest along first.
export function sortNewcomers(list: Newcomer[], by: NewcomerSort): Newcomer[] {
  return list.slice().sort((a, b) => {
    if (by === 'new') return a.id < b.id ? 1 : a.id > b.id ? -1 : 0
    if (by === 'risk') {
      const r = RISK_RANK[a.risk] - RISK_RANK[b.risk]
      if (r !== 0) return r
    }
    return b.day - a.day || a.full_name.localeCompare(b.full_name)
  })
}

// Adaptation runs D-3..D+90: the share of that span already behind.
export function adaptationProgress(day: number): number {
  return Math.min(1, Math.max(0, (day + 3) / 93))
}
