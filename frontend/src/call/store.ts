import { defineStore } from 'pinia'
import { computed, reactive } from 'vue'
import { VoiceEngine } from '@/lib/audio/voiceEngine'
import { apiWsUrl } from '@/lib/wsUrl'
import { createCallSession, createCallState, type CallSocket, type CallState } from './session'

// WebSocket /api/call on the API base (relative "/api" in the deployed build:
// the same origin as the page, wss on https).
export function callSocketUrl(): string {
  const base = import.meta.env.VITE_API_BASE_URL ?? '/api'
  return apiWsUrl(base, 'call', window.location.origin)
}

// The screen's view of the call. All the logic is in ./session.ts; this store
// only makes its state reactive and wires the real socket and audio engine.
export const useCallStore = defineStore('call', () => {
  const state = reactive<CallState>(createCallState())
  const session = createCallSession(state, {
    openSocket: () => new WebSocket(callSocketUrl()) as CallSocket,
    createEngine: (handlers) => new VoiceEngine(handlers),
  })

  const inCall = computed(() => state.connection === 'open' && !state.ended)
  const busy = computed(() => state.connection === 'connecting')

  // Dev only (see CallApp.vue): paint a finished conversation for screenshots.
  function loadDemo(patch: Partial<CallState>) {
    Object.assign(state, patch)
  }

  return {
    state,
    inCall,
    busy,
    start: session.start,
    ask: session.ask,
    sendText: session.sendText,
    setMuted: session.setMuted,
    setSite: session.setSite,
    hangup: session.hangup,
    disconnect: session.disconnect,
    clear: session.clear,
    loadDemo,
  }
})
