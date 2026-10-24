import { defineStore } from 'pinia'
import { ref } from 'vue'
import { apiListUsers } from '@/api/team'
import type { User } from '@/types/api'

// Active recruiters for the pickers (card, launch, reassignment). hr_lead only:
// the server answers 403 to the others, and they never open a picker.
export const useUsersStore = defineStore('users', () => {
  const recruiters = ref<User[]>([])
  const loaded = ref(false)
  let pending: Promise<void> | null = null

  function loadRecruiters(force = false): Promise<void> {
    if (loaded.value && !force) return Promise.resolve()
    pending ??= apiListUsers({ role: 'recruiter', active: true })
      .then((items) => {
        recruiters.value = items
        loaded.value = true
      })
      .finally(() => {
        pending = null
      })
    return pending
  }

  return { recruiters, loaded, loadRecruiters }
})
