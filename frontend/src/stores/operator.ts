import { defineStore } from 'pinia'
import { ref, watch } from 'vue'
import { i18n } from '@/i18n'

const KEY = 'pp.operator'

function readStored(): string {
  try {
    return localStorage.getItem(KEY) ?? ''
  } catch {
    return ''
  }
}

// Who "Take it" assigns the ticket to. Contract v2 has no auth, so the name the
// bot signs replies with ("<Name>, HR (ticket #N):") is the assignee_name the
// cabinet sends. Remembered on this machine; the default is a catalogue entry.
export const useOperatorStore = defineStore('operator', () => {
  const name = ref(readStored() || i18n.global.t('tickets.operator.default'))

  watch(name, (v) => {
    try {
      localStorage.setItem(KEY, v.trim())
    } catch {
      // storage may be blocked: the name then lives until reload
    }
  })

  return { name }
})
