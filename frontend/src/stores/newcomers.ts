import { defineStore } from 'pinia'
import { ref } from 'vue'
import { apiListNewcomers } from '@/api/newcomers'
import type { Newcomer, NewcomerListQuery } from '@/types/api'

// The registry as the server filtered it (site, search). Chips and sorting
// are applied on top by the page (lib/newcomerFilters.ts).
export const useNewcomersStore = defineStore('newcomers', () => {
  const items = ref<Newcomer[]>([])
  const loaded = ref(false)
  const loading = ref(false)
  const error = ref<unknown>(null)
  // Only the answer to the latest request lands: typing in the search fires
  // several, and they may come back out of order.
  let ticket = 0

  async function load(query: NewcomerListQuery): Promise<void> {
    const mine = ++ticket
    loading.value = true
    error.value = null
    try {
      const r = await apiListNewcomers(query)
      if (mine !== ticket) return
      items.value = r.items
      loaded.value = true
    } catch (e) {
      if (mine === ticket) error.value = e
    } finally {
      if (mine === ticket) loading.value = false
    }
  }

  return { items, loaded, loading, error, load }
})
