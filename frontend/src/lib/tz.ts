// Time zones of newcomers (api_contract_auth.md, time zone section). Pure, no Vue: the
// node tests load it directly. `fire_at` is an instant; the cabinet shows it
// in the newcomer's zone ("10:00 GTP time (UTC+5)") and in the viewer's own.
import type { Newcomer, TimelineEvent } from '@/types/api'

// Russian zones for the picker; `key` is the i18n key of the city label
// (common.tz.<key>).
export const TZ_OPTIONS = [
  { id: 'Europe/Kaliningrad', key: 'kaliningrad' },
  { id: 'Europe/Moscow', key: 'moscow' },
  { id: 'Europe/Samara', key: 'samara' },
  { id: 'Asia/Yekaterinburg', key: 'yekaterinburg' },
  { id: 'Asia/Omsk', key: 'omsk' },
  { id: 'Asia/Novosibirsk', key: 'novosibirsk' },
  { id: 'Asia/Krasnoyarsk', key: 'krasnoyarsk' },
  { id: 'Asia/Irkutsk', key: 'irkutsk' },
  { id: 'Asia/Yakutsk', key: 'yakutsk' },
  { id: 'Asia/Vladivostok', key: 'vladivostok' },
  { id: 'Asia/Magadan', key: 'magadan' },
  { id: 'Asia/Kamchatka', key: 'kamchatka' },
] as const

export function tzKey(id: string): string | null {
  return TZ_OPTIONS.find((o) => o.id === id)?.key ?? null
}

// Wall-clock parts of an instant in a zone.
function parts(tz: string, at: Date): Record<string, string> {
  const fmt = new Intl.DateTimeFormat('en-GB', {
    timeZone: tz,
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hourCycle: 'h23',
  })
  const out: Record<string, string> = {}
  for (const p of fmt.formatToParts(at)) out[p.type] = p.value
  return out
}

// Offset of the zone from UTC at that instant, in minutes (UTC+5 → 300).
export function utcOffsetMinutes(tz: string, at: Date): number {
  const p = parts(tz, at)
  const asUtc = Date.UTC(+p.year, +p.month - 1, +p.day, +p.hour, +p.minute)
  const whole = Math.floor(at.getTime() / 60_000) * 60_000
  return Math.round((asUtc - whole) / 60_000)
}

export function offsetLabel(minutes: number): string {
  const sign = minutes < 0 ? '-' : '+'
  const abs = Math.abs(minutes)
  const h = Math.floor(abs / 60)
  const m = abs % 60
  return `UTC${sign}${h}${m ? `:${String(m).padStart(2, '0')}` : ''}`
}

export function tzOffsetLabel(tz: string, at: Date = new Date()): string {
  return offsetLabel(utcOffsetMinutes(tz, at))
}

// "HH:MM" of the instant in the zone.
export function clockIn(tz: string, iso: string): string {
  const p = parts(tz, new Date(iso))
  return `${p.hour}:${p.minute}`
}

// "YYYY-MM-DD" of the instant in the zone.
export function dateIn(tz: string, iso: string): string {
  const p = parts(tz, new Date(iso))
  return `${p.year}-${p.month}-${p.day}`
}

export function isValidTz(tz: string): boolean {
  try {
    new Intl.DateTimeFormat('en-GB', { timeZone: tz })
    return true
  } catch {
    return false
  }
}

// The zone the bot uses for the newcomer: their own, else the site's; null —
// the backend sends neither yet.
export function effectiveTz(n: Pick<Newcomer, 'tz' | 'site_tz'>): string | null {
  return n.tz || n.site_tz || null
}

// The nearest step still to be sent (scheduled or held for the go-ahead).
export function nextStep(events: readonly TimelineEvent[]): TimelineEvent | null {
  let best: TimelineEvent | null = null
  for (const e of events) {
    if (e.status !== 'scheduled' && e.status !== 'held') continue
    if (!best || Date.parse(e.fire_at) < Date.parse(best.fire_at)) best = e
  }
  return best
}
