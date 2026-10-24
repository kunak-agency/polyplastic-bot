import { defineStore } from 'pinia'
import { computed, ref, watch } from 'vue'
import { i18n } from '@/i18n'
import { useAuthStore } from '@/stores/auth'

const KEY = 'pp.operator'

function readStored(): string {
  try {
    return localStorage.getItem(KEY) ?? ''
  } catch {
    return ''
  }
}

// Who acts in the cabinet: the assignee of "Take it", the signature of a reply
// ("<Name>, HR (ticket #N):"), updated_by of media and documents.
// With FEATURE_AUTH it is the signed-in user's full_name, read-only: the server
// takes the name from the session anyway. Without auth it is the name kept on
// this machine (the default is a catalogue entry), as before.
export const useOperatorStore = defineStore('operator', () => {
  const auth = useAuthStore()
  const stored = ref(readStored() || i18n.global.t('tickets.operator.default'))

  watch(stored, (v) => {
    try {
      localStorage.setItem(KEY, v.trim())
    } catch {
      // storage may be blocked: the name then lives until reload
    }
  })

  // Signed in: the name comes from the session and cannot be changed here.
  const locked = computed(() => !!auth.user)
  const userId = computed(() => auth.user?.id ?? null)
  const name = computed<string>({
    get: () => auth.user?.full_name || auth.user?.login || stored.value,
    set: (v) => {
      if (!locked.value) stored.value = v
    },
  })

  return { name, locked, userId }
})
