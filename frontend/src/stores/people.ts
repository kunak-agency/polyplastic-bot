import { defineStore } from 'pinia'
import { ref } from 'vue'
import { apiCreatePerson, apiListPeople } from '@/api/people'
import { upsertPerson } from '@/lib/people'
import type { Person, PersonCreate, PersonRole } from '@/types/api'

// People directory per role for the pickers (launch dialog, newcomer card).
// Loaded once per role; a created person is added in place.
export const usePeopleStore = defineStore('people', () => {
  const byRole = ref<Record<PersonRole, Person[]>>({ manager: [], mentor: [], recruiter: [] })
  const loaded = new Set<PersonRole>()
  const pending = new Map<PersonRole, Promise<void>>()

  function load(role: PersonRole, force = false): Promise<void> {
    if (loaded.has(role) && !force) return Promise.resolve()
    let p = pending.get(role)
    if (!p) {
      p = apiListPeople(role)
        .then((items) => {
          byRole.value[role] = items
          loaded.add(role)
        })
        .finally(() => pending.delete(role))
      pending.set(role, p)
    }
    return p
  }

  async function create(body: PersonCreate): Promise<Person> {
    const p = await apiCreatePerson(body)
    byRole.value[body.role] = upsertPerson(byRole.value[body.role], p)
    return p
  }

  return { byRole, load, create }
})
