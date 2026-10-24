// Reminders to the newcomer (api_contract.md, "Nudges", agent A): the answer of
// the "Remind" button and the history rows of the card. Pure, types-only
// imports: the node tests load it directly.
import type { CatchupProgress, NudgeItem, NudgeResult } from '@/types/api'

// Two auto reminders per question; manual ones add to the same count.
export const AUTO_NUDGES = 2

// What the cabinet says after "Remind". `text` is the server's own message
// when it sent one (202: "will go at 08:00 newcomer time"); `key` is the
// catalog fallback under dashboard.remind.*.
export interface RemindOutcome {
  tone: 'success' | 'info' | 'error'
  key: 'sent' | 'planned' | 'failed' | 'nothing'
  text: string | null
}

export function remindOutcome(
  r: NudgeResult | null | undefined,
  httpStatus: number,
): RemindOutcome {
  const text = r?.message?.trim() || null
  // An older backend answered 204 without a body: it went.
  if (!r || !r.status) return { tone: 'success', key: 'sent', text: null }
  if (r.status === 'failed') return { tone: 'error', key: 'failed', text }
  if (r.status === 'planned' || httpStatus === 202) return { tone: 'info', key: 'planned', text }
  // "Sent" has the cabinet's own short word; the server text is longer.
  return { tone: 'success', key: 'sent', text: null }
}

// 409 is not a failure: nothing to remind about, not connected or the bot is
// off. Shown softly, with the server's words when it gave them.
export function remindConflict(status: number, serverMessage?: string): RemindOutcome | null {
  if (status !== 409) return null
  return { tone: 'info', key: 'nothing', text: serverMessage?.trim() || null }
}

export interface NudgeLine {
  kind: NudgeItem['kind']
  // "1/2"; null for "contacted".
  count: string | null
  // "24.10 13:05" in the newcomer's zone.
  when: string
  state: 'planned' | 'failed' | null
  title: string | null
}

// "24.10 13:05" of the instant in the zone.
function stamp(tz: string, iso: string): string {
  const out: Record<string, string> = {}
  const fmt = new Intl.DateTimeFormat('en-GB', {
    timeZone: tz,
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hourCycle: 'h23',
  })
  for (const p of fmt.formatToParts(new Date(iso))) out[p.type] = p.value
  return `${out.day}.${out.month} ${out.hour}:${out.minute}`
}

export function nudgeLine(item: NudgeItem, tz: string): NudgeLine {
  const zone = tz || 'Europe/Moscow'
  const when = stamp(zone, item.at)
  const state = item.planned
    ? 'planned'
    : !item.delivered && item.kind !== 'contacted'
      ? 'failed'
      : null
  return {
    kind: item.kind,
    count: item.kind === 'contacted' ? null : `${item.n}/${Math.max(AUTO_NUDGES, item.n)}`,
    when,
    state,
    title: item.event_title?.trim() || null,
  }
}

// History newest first, as the card shows it.
export function nudgeHistory(items: readonly NudgeItem[]): NudgeItem[] {
  return items.slice().sort((a, b) => Date.parse(b.at) - Date.parse(a.at))
}

// "Catching up N of M" / "Caught up": null when there is no queue.
export function catchupState(
  c: CatchupProgress | null | undefined,
): { kind: 'progress' | 'done'; done: number; total: number } | null {
  if (!c || c.total <= 0) return null
  const finished = c.finished === true || c.done >= c.total
  return { kind: finished ? 'done' : 'progress', done: Math.min(c.done, c.total), total: c.total }
}
