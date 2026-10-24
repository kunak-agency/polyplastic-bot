// Wave 4: tone row in the newcomer card (#30), quote and tone in the ticket
// card (#26), "what was done" on resolve (#31). Pure helpers only.
import { test } from 'node:test'
import assert from 'node:assert/strict'
import type { NewcomerTone } from '../src/types/api.ts'
import { RECENT_TONE_MAX, TONE_ICON, isSentiment, recentToneRow } from '../src/lib/tone.ts'
import {
  DONE_NOTE_MAX,
  doneNoteBlock,
  foldQuote,
  normDoneNote,
  visibleQuote,
} from '../src/lib/tickets.ts'

function tone(at: string, sentiment: string, kind: 'survey' | 'question' = 'survey'): NewcomerTone {
  return { at, kind, sentiment } as NewcomerTone
}

test('recent tone: oldest to newest, at most five, unknown dropped, empty without data', () => {
  assert.deepEqual(recentToneRow(undefined), [])
  assert.deepEqual(recentToneRow(null), [])
  assert.deepEqual(recentToneRow([]), [])
  const list = [
    tone('2026-10-24T10:00:00+03:00', 'neg'),
    tone('2026-10-23T10:00:00+03:00', 'bogus'),
    tone('2026-10-22T10:00:00+03:00', 'neu', 'question'),
    tone('2026-10-21T10:00:00+03:00', 'pos'),
    tone('2026-10-20T10:00:00+03:00', 'neu'),
    tone('2026-10-19T10:00:00+03:00', 'neg'),
    tone('2026-10-18T10:00:00+03:00', 'pos'),
  ]
  const row = recentToneRow(list)
  assert.equal(row.length, RECENT_TONE_MAX)
  assert.equal(row[0].at, '2026-10-19T10:00:00+03:00')
  assert.equal(row[row.length - 1].at, '2026-10-24T10:00:00+03:00')
  assert.ok(row.every((x) => isSentiment(x.sentiment)))
  // icons only, no numbers
  assert.ok(Object.values(TONE_ICON).every((i) => !/\d/.test(i)))
})

test('quote: none for F10 and F5/F9, folded when long', () => {
  assert.equal(visibleQuote({ category: 'F10', quote: 'text' }), null)
  assert.equal(visibleQuote({ category: 'F5', quote: 'text', restricted: true }), null)
  assert.equal(visibleQuote({ category: 'F9', quote: 'text' }), null)
  assert.equal(visibleQuote({ category: 'F2', quote: '   ' }), null)
  assert.equal(visibleQuote({ category: 'F2', quote: ' no pass ' }), 'no pass')
  assert.deepEqual(foldQuote('short'), { short: 'short', folded: false })
  const long = 'word '.repeat(100).trim()
  const f = foldQuote(long)
  assert.equal(f.folded, true)
  assert.ok(f.short.endsWith('…'))
  assert.ok(f.short.length <= 221)
  assert.ok(!f.short.slice(0, -1).endsWith(' '))
})

test('done note: not sent for anonymous and F5/F9/F10, server flag respected', () => {
  const base = { anonymous: false, restricted: false }
  assert.equal(doneNoteBlock({ ...base, category: 'F2' }), null)
  assert.equal(doneNoteBlock({ ...base, category: 'question', done_note_reaches: true }), null)
  assert.equal(doneNoteBlock({ ...base, category: 'F2', anonymous: true }), 'anonymous')
  assert.equal(doneNoteBlock({ ...base, category: 'F5' }), 'confidential')
  assert.equal(doneNoteBlock({ ...base, category: 'F9' }), 'confidential')
  assert.equal(doneNoteBlock({ ...base, category: 'F10' }), 'confidential')
  assert.equal(doneNoteBlock({ ...base, category: 'F2', done_note_reaches: false }), 'confidential')
  assert.equal(normDoneNote('  a\n b\t c  '), 'a b c')
  assert.equal(DONE_NOTE_MAX, 300)
})
