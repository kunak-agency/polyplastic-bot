// Reminders (src/lib/nudges.ts): the "Remind" answer and history rows.
import { test } from 'node:test'
import assert from 'node:assert/strict'
import type { NudgeItem } from '../src/types/api.ts'
import {
  catchupState,
  nudgeHistory,
  nudgeLine,
  remindConflict,
  remindOutcome,
} from '../src/lib/nudges.ts'

test('remind outcome: sent, planned with the server text, failed, 409 soft', () => {
  assert.deepEqual(remindOutcome({ status: 'sent', message: 'x' }, 200), {
    tone: 'success',
    key: 'sent',
    text: null,
  })
  assert.deepEqual(remindOutcome({ status: 'planned', message: ' Planned at 08:00 ' }, 202), {
    tone: 'info',
    key: 'planned',
    text: 'Planned at 08:00',
  })
  assert.equal(remindOutcome({ status: 'failed' }, 200).tone, 'error')
  // An older backend: 204 without a body.
  assert.equal(remindOutcome(null, 204).key, 'sent')
  assert.deepEqual(remindConflict(409, 'nothing'), {
    tone: 'info',
    key: 'nothing',
    text: 'nothing',
  })
  assert.equal(remindConflict(409)?.text, null)
  assert.equal(remindConflict(500, 'x'), null)
})

function item(extra: Partial<NudgeItem>): NudgeItem {
  return {
    id: 'a',
    event_id: 'e',
    event_title: 'Access',
    kind: 'auto',
    n: 1,
    at: '2026-10-24T10:05:00Z',
    planned: false,
    delivered: true,
    created_by: null,
    ...extra,
  }
}

test('history line: count, time in the newcomer zone, state', () => {
  const l = nudgeLine(item({}), 'Europe/Moscow')
  assert.equal(l.count, '1/2')
  assert.equal(l.when, '24.10 13:05')
  assert.equal(l.state, null)
  assert.equal(l.title, 'Access')
  assert.equal(nudgeLine(item({ n: 3, kind: 'manual' }), 'Asia/Yekaterinburg').count, '3/3')
  assert.equal(nudgeLine(item({ n: 3 }), 'Asia/Yekaterinburg').when, '24.10 15:05')
  assert.equal(nudgeLine(item({ planned: true, delivered: false }), 'UTC').state, 'planned')
  assert.equal(nudgeLine(item({ delivered: false }), 'UTC').state, 'failed')
  const c = nudgeLine(item({ kind: 'contacted', delivered: false, n: 0 }), 'UTC')
  assert.equal(c.count, null)
  assert.equal(c.state, null)
  const h = nudgeHistory([
    item({ id: '1', at: '2026-10-24T10:00:00Z' }),
    item({ id: '2', at: '2026-10-25T10:00:00Z' }),
  ])
  assert.deepEqual(
    h.map((x) => x.id),
    ['2', '1'],
  )
})

test('catch-up state', () => {
  assert.equal(catchupState(null), null)
  assert.equal(catchupState({ done: 0, total: 0 }), null)
  assert.deepEqual(catchupState({ done: 1, total: 4 }), { kind: 'progress', done: 1, total: 4 })
  assert.equal(catchupState({ done: 4, total: 4 })?.kind, 'done')
  assert.equal(catchupState({ done: 2, total: 4, finished: true })?.kind, 'done')
})
