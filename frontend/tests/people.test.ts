// People directory helpers (src/lib/people.ts): search and duplicates in the
// manager / mentor picker of the launch dialog.
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { exactPerson, filterPeople, normName, upsertPerson } from '../src/lib/people.ts'
import type { Person } from '../src/types/api.ts'

const p = (id: string, full_name: string, active = true): Person => ({
  id,
  full_name,
  role: 'manager',
  has_login: false,
  active,
})

const LIST = [p('1', 'Смирнов А.'), p('2', 'Семёнова Т.'), p('3', 'Старый Б.', false)]

test('names compare without case, extra spaces and the yo letter', () => {
  assert.equal(normName('  СЕМЁНОВА   т. '), normName('семенова т.'))
})

test('search by a part of the name, inactive hidden unless selected', () => {
  assert.deepEqual(
    filterPeople(LIST, 'сем').map((x) => x.id),
    ['2'],
  )
  assert.deepEqual(
    filterPeople(LIST, '').map((x) => x.id),
    ['2', '1'],
  )
  assert.deepEqual(
    filterPeople(LIST, 'стар', '3').map((x) => x.id),
    ['3'],
  )
})

test('exact match offers the existing person instead of a duplicate', () => {
  assert.equal(exactPerson(LIST, ' смирнов а. ')?.id, '1')
  assert.equal(exactPerson(LIST, 'Смирнов'), null)
  assert.equal(exactPerson(LIST, '  '), null)
})

test('a created person is added, a returned existing one replaced', () => {
  const added = upsertPerson(LIST, p('4', 'Новый В.'))
  assert.equal(added.length, 4)
  const same = upsertPerson(LIST, { ...LIST[0], has_login: true })
  assert.equal(same.length, 3)
  assert.equal(same[0].has_login, true)
})
