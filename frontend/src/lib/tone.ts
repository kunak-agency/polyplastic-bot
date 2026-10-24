// Tone of a newcomer's text (wave 4, #26/#30): a hint, never a score. Only an
// icon and a word, no numbers. Pure, so the node tests load it directly.
import type { NewcomerTone, Sentiment } from '@/types/api'

export const TONE_ICON: Record<Sentiment, string> = {
  pos: '🙂',
  neu: '😐',
  neg: '☹',
}

export function isSentiment(v: unknown): v is Sentiment {
  return v === 'pos' || v === 'neu' || v === 'neg'
}

// How many recent texts the newcomer card shows.
export const RECENT_TONE_MAX = 5

// The server sends the newest first; the row reads left to right, oldest to
// newest. Unknown sentiments are dropped; an empty row means "show nothing".
export function recentToneRow(list: readonly NewcomerTone[] | null | undefined): NewcomerTone[] {
  if (!list) return []
  return list
    .filter((x) => isSentiment(x.sentiment))
    .slice(0, RECENT_TONE_MAX)
    .reverse()
}
