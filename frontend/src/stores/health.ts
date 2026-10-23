import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { apiGetHealth } from '@/api/health'
import type { HealthResponse } from '@/types/api'

// unknown — no answer yet; ok — API and its dependencies are up; degraded — the
// API answered but db or rag is down; down — the API itself did not answer.
export type ApiStatus = 'unknown' | 'ok' | 'degraded' | 'down'

export const useHealthStore = defineStore('health', () => {
  const last = ref<HealthResponse | null>(null)
  const failed = ref(false)
  const checkedAt = ref<Date | null>(null)
  let inFlight = false

  const status = computed<ApiStatus>(() => {
    if (failed.value) return 'down'
    const h = last.value
    if (!h) return 'unknown'
    if (h.status !== 'ok') return 'down'
    return h.db === 'ok' && h.rag === 'ok' ? 'ok' : 'degraded'
  })

  // A failed probe is a state, not an error to surface: the badge shows it.
  async function check(): Promise<void> {
    if (inFlight) return
    inFlight = true
    try {
      last.value = await apiGetHealth()
      failed.value = false
    } catch {
      last.value = null
      failed.value = true
    } finally {
      checkedAt.value = new Date()
      inFlight = false
    }
  }

  return { last, status, checkedAt, check }
})
