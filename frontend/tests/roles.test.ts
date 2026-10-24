// Role matrix of the cabinet (src/lib/roles.ts).
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { canOpen, homeRoute } from '../src/lib/roles.ts'

test('auth off: everything as before, without user-bound screens', () => {
  assert.equal(canOpen(null, 'overview'), true)
  assert.equal(canOpen(null, 'manager'), true)
  assert.equal(canOpen(null, 'dashboard'), false)
  assert.equal(canOpen(null, 'settings'), true)
  assert.equal(homeRoute(null), 'overview')
})

test('manager sees only his newcomers', () => {
  assert.equal(canOpen('manager', 'manager'), true)
  for (const r of ['overview', 'newcomers', 'newcomer', 'tickets', 'settings', 'dashboard'])
    assert.equal(canOpen('manager', r), false, r)
  assert.equal(homeRoute('manager'), 'manager')
})

test('recruiter: all but users in settings, starts on the dashboard', () => {
  assert.equal(canOpen('recruiter', 'settings'), false)
  assert.equal(canOpen('recruiter', 'dashboard'), true)
  assert.equal(canOpen('recruiter', 'tickets'), true)
  assert.equal(homeRoute('recruiter'), 'dashboard')
})

test('hr_lead sees everything', () => {
  for (const r of ['dashboard', 'overview', 'manager', 'settings'])
    assert.equal(canOpen('hr_lead', r), true, r)
  assert.equal(homeRoute('hr_lead'), 'dashboard')
})
