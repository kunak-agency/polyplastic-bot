import { defineStore } from 'pinia'
import { ref } from 'vue'
import { apiListScenarios } from '@/api/scenarios'
import type { Scenario } from '@/types/api'

// Scenario list: the registry page and the launch dialog's scenario picker.
export const useScenariosStore = defineStore('scenarios', () => {
  const items = ref<Scenario[]>([])
  const loaded = ref(false)
  const loading = ref(false)
  const error = ref<unknown>(null)

  async function load(): Promise<void> {
    loading.value = true
    error.value = null
    try {
      items.value = (await apiListScenarios()).items
      loaded.value = true
    } catch (e) {
      error.value = e
    } finally {
      loading.value = false
    }
  }

  return { items, loaded, loading, error, load }
})
