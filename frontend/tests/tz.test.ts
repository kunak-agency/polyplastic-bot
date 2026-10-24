// Time zones (src/lib/tz.ts): the newcomer's local time of a step.
import { test } from 'node:test'
import assert from 'node:assert/strict'
import type { TimelineEvent } from '../src/types/api.ts'
import {
  clockIn,
  dateIn,
  effectiveTz,
  isValidTz,
  nextStep,
  offsetLabel,
  tzOffsetLabel,
  utcOffsetMinutes,
} from '../src/lib/tz.ts'

const AT = new Date('2026-10-26T05:00:00Z')

test('offsets of Russian zones', () => {
  assert.equal(utcOffsetMinutes('Asia/Yekaterinburg', AT), 300)
  assert.equal(utcOffsetMinutes('Europe/Moscow', AT), 180)
  assert.equal(tzOffsetLabel('Asia/Yekaterinburg', AT), 'UTC+5')
  assert.equal(offsetLabel(-90), 'UTC-1:30')
  assert.equal(offsetLabel(0), 'UTC+0')
})

test('10:00 at GTP (UTC+5) is 05:00Z and 08:00 in Moscow, no double shift', () => {
  const iso = '2026-10-26T10:00:00+05:00'
  assert.equal(clockIn('Asia/Yekaterinburg', iso), '10:00')
  assert.equal(clockIn('Europe/Moscow', iso), '08:00')
  assert.equal(clockIn('UTC', iso), '05:00')
  assert.equal(dateIn('Asia/Vladivostok', '2026-10-26T20:00:00+03:00'), '2026-10-27')
})

test('effective zone: the newcomer’s, else the site’s', () => {
  assert.equal(effectiveTz({ tz: 'Asia/Omsk', site_tz: 'Europe/Moscow' }), 'Asia/Omsk')
  assert.equal(effectiveTz({ tz: null, site_tz: 'Europe/Moscow' }), 'Europe/Moscow')
  assert.equal(effectiveTz({}), null)
  // The server's effective zone wins: without FEATURE_TZ it stays the site's.
  assert.equal(
    effectiveTz({ tz: 'Asia/Omsk', site_tz: 'Europe/Moscow', effective_tz: 'Europe/Moscow' }),
    'Europe/Moscow',
  )
  assert.equal(isValidTz('Asia/Yekaterinburg'), true)
  assert.equal(isValidTz('Mars/Olympus'), false)
})

function ev(id: string, fire_at: string, status: TimelineEvent['status']): TimelineEvent {
  return {
    event_id: id,
    step_id: id,
    day_offset: 0,
    title: id,
    kind: 'text',
    fire_at,
    status,
    answer: null,
    text: null,
  }
}

test('next step: the earliest scheduled or held', () => {
  const list = [
    ev('sent', '2026-10-20T10:00:00+03:00', 'sent'),
    ev('later', '2026-10-28T10:00:00+03:00', 'scheduled'),
    ev('held', '2026-10-27T10:00:00+03:00', 'held'),
  ]
  assert.equal(nextStep(list)?.event_id, 'held')
  assert.equal(nextStep([list[0]]), null)
})
