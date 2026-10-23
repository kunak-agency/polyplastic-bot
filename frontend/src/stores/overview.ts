import { defineStore } from 'pinia'
import { ref } from 'vue'
import { apiGetOverview } from '@/api/overview'
import type { Overview, SiteCode } from '@/types/api'

// The overview of the site picked in the topbar. Only the answer to the latest
// request lands: switching sites quickly must not show a stale site's numbers.
export const useOverviewStore = defineStore('overview', () => {
  const data = ref<Overview | null>(null)
  const loading = ref(false)
  const error = ref<unknown>(null)
  let ticket = 0

  async function load(site: SiteCode | null): Promise<void> {
    const mine = ++ticket
    loading.value = true
    error.value = null
    try {
      const r = await apiGetOverview(site)
      if (mine === ticket) data.value = r
    } catch (e) {
      if (mine === ticket) error.value = e
    } finally {
      if (mine === ticket) loading.value = false
    }
  }

  return { data, loading, error, load }
})
