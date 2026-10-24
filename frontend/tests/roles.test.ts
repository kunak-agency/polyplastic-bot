// Role matrix of the cabinet (src/lib/roles.ts) and the menu it gives (src/router/nav.ts).
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { canOpen, homeRoute, inMenu, type Viewer } from '../src/lib/roles.ts'
import { NAV, NAV_FOOTER } from '../src/router/nav.ts'

function menu(viewer: Viewer): string[] {
  return [...NAV, ...NAV_FOOTER].filter((it) => inMenu(viewer, it.routeName)).map((it) => it.id)
}

test('menu by role', () => {
  assert.deepEqual(menu('hr_lead'), [
    'dashboard',
    'tickets',
    'newcomers',
    'team',
    'analytics',
    'overview',
    'scenarios',
    'knowledge',
    'media',
    'settings',
  ])
  assert.deepEqual(menu('recruiter'), [
    'dashboard',
    'tickets',
    'newcomers',
    'analytics',
    'overview',
    'scenarios',
    'knowledge',
    'media',
  ])
  assert.deepEqual(menu('manager'), ['manager'])
  // Auth off: as before — "My employees" with the picker and Settings, no
  // user-bound screens (Today, Team).
  assert.deepEqual(menu(null), [
    'tickets',
    'newcomers',
    'manager',
    'analytics',
    'overview',
    'scenarios',
    'knowledge',
    'media',
    'settings',
  ])
})

test('auth off: everything as before, without user-bound screens', () => {
  assert.equal(canOpen(null, 'overview'), true)
  assert.equal(canOpen(null, 'manager'), true)
  assert.equal(canOpen(null, 'dashboard'), false)
  assert.equal(canOpen(null, 'team'), false)
  assert.equal(canOpen(null, 'settings'), true)
  assert.equal(homeRoute(null), 'overview')
})

test('manager sees only his newcomers', () => {
  assert.equal(canOpen('manager', 'manager'), true)
  for (const r of ['overview', 'newcomers', 'newcomer', 'tickets', 'settings', 'dashboard', 'team'])
    assert.equal(canOpen('manager', r), false, r)
  assert.equal(homeRoute('manager'), 'manager')
})

test('recruiter: no settings, team or head-of-unit view; starts on the dashboard', () => {
  for (const r of ['settings', 'team', 'manager']) assert.equal(canOpen('recruiter', r), false, r)
  for (const r of ['dashboard', 'tickets', 'newcomers', 'newcomer', 'analytics'])
    assert.equal(canOpen('recruiter', r), true, r)
  assert.equal(homeRoute('recruiter'), 'dashboard')
})

test('hr_lead opens everything; the head-of-unit view only by link', () => {
  for (const r of ['dashboard', 'overview', 'manager', 'settings', 'team'])
    assert.equal(canOpen('hr_lead', r), true, r)
  assert.equal(inMenu('hr_lead', 'manager'), false)
  assert.equal(inMenu('hr_lead', 'team'), true)
  assert.equal(homeRoute('hr_lead'), 'dashboard')
})
