import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { apiListTickets } from '@/api/tickets'
import { activeCount } from '@/lib/tickets'
import type { SiteCode, Ticket } from '@/types/api'

// The queue as the server filtered it by site; views (lib/tickets.ts) are
// applied on top by the page. The sidebar badge reads the same list, so the
// number next to "Tickets" always matches the queue the HR sees.
export const useTicketsStore = defineStore('tickets', () => {
  const items = ref<Ticket[]>([])
  const loaded = ref(false)
  const loading = ref(false)
  const error = ref<unknown>(null)
  const site = ref<SiteCode | null>(null)
  // Only the answer to the latest request lands (site switches race).
  let ticket = 0

  async function load(forSite: SiteCode | null = site.value): Promise<void> {
    const mine = ++ticket
    site.value = forSite
    loading.value = true
    error.value = null
    try {
      const r = await apiListTickets({ site: forSite })
      if (mine !== ticket) return
      items.value = r.items
      loaded.value = true
    } catch (e) {
      if (mine === ticket) error.value = e
    } finally {
      if (mine === ticket) loading.value = false
    }
  }

  const openCount = computed(() => activeCount(items.value))

  return { items, loaded, loading, error, site, load, openCount }
})
