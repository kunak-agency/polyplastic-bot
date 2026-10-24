import { defineStore } from 'pinia'
import { ref } from 'vue'
import { apiGetIndicators } from '@/api/indicators'
import type { IndicatorPeriodKey, Indicators, SiteCode } from '@/types/api'

// "Indicators" of the site picked in the topbar and the period of the route.
// Only the answer to the latest request lands: switching quickly must not
// show a stale site's or period's numbers.
export const useIndicatorsStore = defineStore('indicators', () => {
  const data = ref<Indicators | null>(null)
  const loading = ref(false)
  const error = ref<unknown>(null)
  let ticket = 0

  async function load(site: SiteCode | null, period: IndicatorPeriodKey): Promise<void> {
    const mine = ++ticket
    loading.value = true
    error.value = null
    try {
      const r = await apiGetIndicators(site, period)
      if (mine === ticket) data.value = r
    } catch (e) {
      if (mine === ticket) error.value = e
    } finally {
      if (mine === ticket) loading.value = false
    }
  }

  return { data, loading, error, load }
})
