// The call placed from this browser: socket lifecycle, the voice engine, the
// transcript and the phase. Framework-free on purpose: the Pinia store
// (call/store.ts) hands it a reactive state object and the real socket and
// engine; tests (tests/callSession.test.ts) hand it plain objects and fakes.
//
// The voice part (gen / clearedGen / openTurns, playback.mark, playback.clear)
// is the logic of hack-ru-clinik frontend/src/stores/call.ts, kept as is.
import { rmsToLevel } from '../lib/audio/pcm.ts'
import type {
  CallClientMessage,
  CallConnection,
  CallInput,
  CallMessage,
  CallPhase,
  CallProblem,
  CallServerEvent,
  CallSite,
  CallSource,
  MicState,
} from './protocol.ts'

// The subset of the browser WebSocket the session relies on.
export interface CallSocket {
  readonly readyState: number
  binaryType: BinaryType
  onopen: ((ev: Event) => void) | null
  onmessage: ((ev: MessageEvent) => void) | null
  onerror: ((ev: Event) => void) | null
  onclose: ((ev: CloseEvent) => void) | null
  send(data: string | ArrayBuffer): void
  close(code?: number, reason?: string): void
}

export interface EngineHandlers {
  onMicFrame(pcm: ArrayBuffer): void
  onMicLevel(rms: number): void
  onPlaying(playing: boolean): void
  onTurnPlayed(turn: number): void
}

// The subset of lib/audio/voiceEngine.ts VoiceEngine the session uses.
export interface CallEngine {
  open(): Promise<void>
  setMuted(muted: boolean): void
  play(data: ArrayBuffer): void
  markEnd(turn: number): void
  clear(): void
  closeAfterPlayback(maxMs?: number): Promise<void>
  close(): Promise<void>
}

export interface CallDeps {
  openSocket(): CallSocket
  createEngine(handlers: EngineHandlers): CallEngine
  now?: () => number
}

export interface CallState {
  connection: CallConnection
  phase: CallPhase
  input: CallInput
  site: CallSite | null
  callId: string | null
  messages: CallMessage[]
  mic: MicState
  micLevel: number
  muted: boolean
  userSpeaking: boolean
  botPlaying: boolean
  // ms timestamp of the moment the line opened; drives the call timer.
  startedAt: number | null
  endedAt: number | null
  problem: CallProblem | null
  // Server error code behind problem = 'server'.
  errorCode: string | null
  ended: boolean
}

export function createCallState(): CallState {
  return {
    connection: 'idle',
    phase: 'idle',
    input: 'voice',
    site: 'GrPP',
    callId: null,
    messages: [],
    mic: 'off',
    micLevel: 0,
    muted: false,
    userSpeaking: false,
    botPlaying: false,
    startedAt: null,
    endedAt: null,
    problem: null,
    errorCode: null,
    ended: false,
  }
}

const OPEN = 1
// After `hangup` the server answers call.end and closes the socket itself; if
// it does not within this window, the client closes it.
const HANGUP_CLOSE_TIMEOUT_MS = 3000
// A socket to a host that accepts TCP but never answers the upgrade otherwise
// sits in CONNECTING indefinitely.
const CONNECT_TIMEOUT_MS = 8000

export type MicErrorLike = { kind: string }

function micKind(e: unknown): MicState {
  const kind = e && typeof e === 'object' && 'kind' in e ? (e as MicErrorLike).kind : 'failed'
  return kind === 'denied' || kind === 'notFound' || kind === 'unsupported' ? kind : 'failed'
}

export interface CallSession {
  start(opts?: { input?: CallInput; text?: string }): void
  sendText(text: string): boolean
  // A hint or typed question: sent now, or (no call yet) starts one first.
  ask(text: string): void
  setMuted(muted: boolean): void
  setSite(site: CallSite | null): void
  hangup(): void
  disconnect(): void
  clear(): void
  readonly inCall: boolean
  readonly canSend: boolean
}

export function createCallSession(state: CallState, deps: CallDeps): CallSession {
  const now = deps.now ?? (() => Date.now())

  let socket: CallSocket | null = null
  let voice: CallEngine | null = null
  // Engine of an ended call still playing its last phrase (see stopVoice).
  let draining: CallEngine | null = null
  // Barge-in bookkeeping: after playback.clear{gen}, binary frames are dropped
  // until an audio.start with gen >= clearedGen; `acceptAudio` says whether the
  // frames arriving now belong to a live (not cleared) audio.start.
  let clearedGen: number | null = null
  let acceptAudio = false
  let audioTurn: number | null = null
  // Turns whose audio started and has not been reported by playback.mark yet.
  const openTurns = new Set<number>()
  let seq = 0
  let hangupTimer: ReturnType<typeof setTimeout> | null = null
  let connectTimer: ReturnType<typeof setTimeout> | null = null
  // Text to send as soon as the line opens (a hint clicked before the call).
  let queuedText: string | null = null

  const inCall = () => state.connection === 'open' && !state.ended
  const isVoice = () => state.input === 'voice'
  const nextId = () => ++seq

  function reset() {
    state.phase = 'idle'
    state.callId = null
    state.messages = []
    state.mic = 'off'
    state.micLevel = 0
    state.muted = false
    state.userSpeaking = false
    state.botPlaying = false
    state.startedAt = null
    state.endedAt = null
    state.problem = null
    state.errorCode = null
    state.ended = false
    clearedGen = null
    acceptAudio = false
    audioTurn = null
    openTurns.clear()
    queuedText = null
  }

  function send(msg: CallClientMessage): boolean {
    if (!socket || socket.readyState !== OPEN) return false
    socket.send(JSON.stringify(msg))
    return true
  }

  function idlePhase(): CallPhase {
    if (state.ended) return 'ended'
    if (state.botPlaying) return 'speaking'
    return 'listening'
  }

  function sendMark(turn: number, complete: boolean) {
    send({ type: 'playback.mark', turn, complete })
  }

  // ── Engine callbacks ──

  function onMicFrame(pcm: ArrayBuffer) {
    // Nothing goes out before `start`: it is sent in onopen, first.
    if (!socket || socket.readyState !== OPEN || state.ended) return
    socket.send(pcm)
  }

  function onMicLevel(rms: number) {
    const next = rmsToLevel(rms)
    // Fast attack, slow release: the meter does not flicker between frames.
    state.micLevel = next > state.micLevel ? next : state.micLevel * 0.85 + next * 0.15
  }

  function onPlaying(playing: boolean) {
    state.botPlaying = playing
    if (state.ended) return
    if (playing) state.phase = 'speaking'
    // An underrun mid-turn (more audio expected) keeps «speaking».
    else if (!openTurns.size) state.phase = idlePhase()
  }

  // The player passed the mark posted at audio.end{turn}: report it played.
  function onTurnPlayed(turn: number) {
    if (!openTurns.has(turn)) return // cleared meanwhile: already reported false
    sendMark(turn, true)
    // The same turn may already stream its next phrase; keep it open until that ends too.
    if (!(acceptAudio && audioTurn === turn)) openTurns.delete(turn)
    if (!openTurns.size && !state.botPlaying && state.phase === 'speaking') {
      state.phase = idlePhase()
    }
  }

  function onAudioFrame(data: ArrayBuffer) {
    if (!acceptAudio) return // stale generation or no audio.start yet
    voice?.play(data)
  }

  // playback.clear: barge-in.
  function clearPlayback(gen: number) {
    clearedGen = clearedGen === null ? gen : Math.max(clearedGen, gen)
    acceptAudio = false
    voice?.clear()
    for (const turn of openTurns) sendMark(turn, false)
    openTurns.clear()
    if (!state.ended) state.phase = 'listening'
  }

  // graceful: the server ended the call — mic off now, the last phrase plays
  // out. Otherwise everything stops now, a previous call's tail included.
  function stopVoice(graceful: boolean) {
    const engine = voice
    voice = null
    state.micLevel = 0
    if (!graceful && draining) {
      void draining.close()
      draining = null
    }
    if (!engine) return
    if (state.mic === 'live' || state.mic === 'starting') state.mic = 'off'
    if (!graceful) {
      void engine.close()
      return
    }
    draining = engine
    void engine.closeAfterPlayback().then(() => {
      if (draining === engine) draining = null
    })
  }

  // ── Transcript ──

  function applyUserPartial(text: string) {
    const bubble = state.messages.find((m) => m.role === 'user' && m.partial)
    if (bubble) bubble.text = text
    else state.messages.push({ id: nextId(), role: 'user', text, partial: true })
  }

  function applyUserFinal(text: string) {
    // The oldest optimistic bubble (echoes come in send order), else the
    // partial being recognised; otherwise the final is a new bubble.
    const bubble =
      state.messages.find((m) => m.role === 'user' && m.pending) ??
      state.messages.find((m) => m.role === 'user' && m.partial)
    if (bubble) {
      bubble.text = text
      bubble.pending = false
      bubble.partial = false
    } else {
      state.messages.push({ id: nextId(), role: 'user', text })
    }
  }

  function streamingFor(turn: number | undefined): CallMessage | undefined {
    if (turn === undefined) return undefined
    return state.messages.find((m) => m.role === 'bot' && m.turn === turn && m.streaming)
  }

  function applyBotSay(text: string, turn?: number, sources?: CallSource[], filler?: boolean) {
    const clean = Array.isArray(sources) ? sources.filter((s) => s && s.label) : []
    if (filler) {
      state.messages.push({ id: nextId(), role: 'bot', text, turn, filler: true })
      return
    }
    const current = streamingFor(turn)
    if (current) {
      current.text = text
      current.streaming = false
      current.sources = clean
    } else {
      state.messages.push({ id: nextId(), role: 'bot', text, turn, sources: clean })
    }
  }

  function stopStreaming() {
    for (const m of state.messages) m.streaming = false
  }

  // ── Server events ──

  function handle(ev: CallServerEvent) {
    switch (ev.type) {
      case 'call.started':
        state.callId = ev.callId ?? null
        break

      case 'user.partial':
        if (ev.text) applyUserPartial(ev.text)
        break

      case 'user.final':
        state.userSpeaking = false
        applyUserFinal(ev.text)
        state.phase = 'thinking'
        break

      case 'user.speech':
        if (ev.state === undefined && typeof ev.text === 'string') {
          state.userSpeaking = false
          applyUserFinal(ev.text)
          state.phase = 'thinking'
          break
        }
        state.userSpeaking = ev.state === 'start'
        if (ev.state === 'start' && !state.ended) state.phase = 'listening'
        if (ev.state === 'end' && !state.ended) state.phase = 'thinking'
        break

      case 'bot.delta': {
        const current = streamingFor(ev.turn)
        if (current) current.text += ev.text
        else
          state.messages.push({
            id: nextId(),
            role: 'bot',
            turn: ev.turn,
            text: ev.text,
            streaming: true,
          })
        if (isVoice() && !state.botPlaying) state.phase = 'thinking'
        break
      }

      case 'bot.say':
        applyBotSay(ev.text, ev.turn, ev.sources, ev.filler)
        if (ev.filler) break
        // Text: the turn is over once its text arrives. Voice: the phase follows the audio.
        if (!isVoice() && !state.ended) state.phase = 'listening'
        break

      case 'turn.metrics':
        // A voice turn that produced no audio (or it has already played out).
        if (isVoice() && !openTurns.size && !state.botPlaying && state.phase === 'thinking') {
          state.phase = idlePhase()
        }
        break

      case 'audio.start':
        if (clearedGen !== null && ev.gen < clearedGen) {
          acceptAudio = false // late start of a generation already cleared
          break
        }
        acceptAudio = true
        audioTurn = ev.turn
        openTurns.add(ev.turn)
        break

      case 'audio.end':
        if (audioTurn === ev.turn) acceptAudio = false
        if (!openTurns.has(ev.turn)) break // cleared, or its start was stale
        if (voice) voice.markEnd(ev.turn)
        else onTurnPlayed(ev.turn) // no player (text fallback): nothing to wait for
        break

      case 'playback.clear':
        clearPlayback(ev.gen)
        break

      case 'call.end':
        state.ended = true
        state.endedAt = now()
        state.phase = 'ended'
        stopStreaming()
        // Mic off now; the last phrase plays out.
        stopVoice(true)
        break

      case 'error':
        state.errorCode = ev.code
        state.problem = ev.code === 'busy' ? 'busy' : 'server'
        stopStreaming()
        if (ev.code === 'busy') {
          // No line: nothing will happen in this call.
          state.ended = true
          state.endedAt = now()
          state.phase = 'ended'
          stopVoice(false)
        } else {
          state.phase = idlePhase()
        }
        break
    }
  }

  function onMessage(e: MessageEvent) {
    if (e.data instanceof ArrayBuffer) {
      onAudioFrame(e.data)
      return
    }
    if (typeof e.data !== 'string') return
    let ev: CallServerEvent
    try {
      ev = JSON.parse(e.data) as CallServerEvent
    } catch {
      return
    }
    if (ev && typeof ev === 'object' && typeof ev.type === 'string') handle(ev)
  }

  // ── Lifecycle ──

  function clearTimers() {
    if (hangupTimer) clearTimeout(hangupTimer)
    if (connectTimer) clearTimeout(connectTimer)
    hangupTimer = null
    connectTimer = null
  }

  function detach(ws: CallSocket) {
    ws.onopen = null
    ws.onmessage = null
    ws.onerror = null
    ws.onclose = null
  }

  function fail() {
    clearTimers()
    stopVoice(false)
    const ws = socket
    socket = null
    if (ws) {
      detach(ws)
      try {
        ws.close()
      } catch {
        // already closing
      }
    }
    state.connection = 'failed'
    state.problem = 'failed'
    state.phase = 'idle'
  }

  function openSocket() {
    let ws: CallSocket
    try {
      ws = deps.openSocket()
    } catch {
      fail()
      return
    }
    ws.binaryType = 'arraybuffer'
    socket = ws
    connectTimer = setTimeout(fail, CONNECT_TIMEOUT_MS)

    ws.onopen = () => {
      if (connectTimer) clearTimeout(connectTimer)
      connectTimer = null
      state.connection = 'open'
      state.startedAt = now()
      state.phase = 'listening'
      send({ type: 'start', input: state.input, site: state.site })
      const text = queuedText
      queuedText = null
      if (text) sendText(text)
    }
    ws.onmessage = onMessage
    // onerror is always followed by onclose; the state is decided there.
    ws.onerror = () => {}
    ws.onclose = () => {
      clearTimers()
      if (socket === ws) socket = null
      detach(ws)
      stopVoice(true)
      stopStreaming()
      // Never opened: the backend (or the route) is unreachable.
      if (state.startedAt === null && !state.ended) {
        state.connection = 'failed'
        if (!state.problem) state.problem = 'failed'
        state.phase = 'idle'
        return
      }
      if (!state.ended) {
        if (!state.problem) state.problem = 'lost'
        state.ended = true
      }
      if (state.endedAt === null) state.endedAt = now()
      state.connection = 'closed'
      state.phase = 'ended'
    }
  }

  // Call from the click handler: in voice mode the AudioContext is created
  // synchronously inside it (autoplay policies), then the mic is requested;
  // the socket opens only once the mic is live.
  function start(opts: { input?: CallInput; text?: string } = {}) {
    disconnect()
    reset()
    state.input = opts.input ?? 'voice'
    queuedText = opts.text?.trim() || null
    state.connection = 'connecting'
    state.phase = 'connecting'
    if (state.input !== 'voice') {
      openSocket()
      return
    }

    state.mic = 'starting'
    const engine = deps.createEngine({ onMicFrame, onMicLevel, onPlaying, onTurnPlayed })
    voice = engine
    engine.open().then(
      () => {
        if (voice !== engine) return // cancelled (hang-up, new call)
        state.mic = 'live'
        openSocket()
      },
      (e: unknown) => {
        if (voice !== engine) return
        voice = null
        state.mic = micKind(e)
        state.connection = 'idle'
        state.phase = 'idle'
      },
    )
  }

  function sendText(text: string): boolean {
    const trimmed = text.trim()
    if (!trimmed || !inCall()) return false
    // The bubble goes in BEFORE the frame: the echo (user.final) must find it.
    const bubble: CallMessage = { id: nextId(), role: 'user', text: trimmed, pending: true }
    state.messages.push(bubble)
    if (!send({ type: 'user.text', text: trimmed })) {
      state.messages = state.messages.filter((m) => m.id !== bubble.id)
      return false
    }
    state.phase = 'thinking'
    return true
  }

  function ask(text: string) {
    if (!text.trim()) return
    if (inCall()) {
      sendText(text)
      return
    }
    if (state.connection === 'connecting') {
      queuedText = text.trim()
      return
    }
    // The mic is known to be unusable: go straight to text.
    const micBroken = state.mic !== 'off' && state.mic !== 'live' && state.mic !== 'starting'
    start({ input: micBroken ? 'text' : 'voice', text })
  }

  function setMuted(next: boolean) {
    state.muted = next
    voice?.setMuted(next)
    if (next) state.micLevel = 0
  }

  function setSite(site: CallSite | null) {
    // The site goes out with `start`; it is fixed for a call in progress.
    if (state.connection === 'connecting' || inCall()) return
    state.site = site
  }

  function hangup() {
    // Silence at once, mic and speakers.
    stopVoice(false)
    const ws = socket
    if (!ws) {
      disconnect()
      return
    }
    if (!send({ type: 'hangup' })) {
      disconnect()
      return
    }
    state.ended = true
    state.endedAt = now()
    state.phase = 'ended'
    if (hangupTimer) clearTimeout(hangupTimer)
    hangupTimer = setTimeout(() => {
      if (socket === ws) ws.close()
    }, HANGUP_CLOSE_TIMEOUT_MS)
  }

  // Tears the socket down without waiting for the server (page left, new call).
  function disconnect() {
    clearTimers()
    stopVoice(false)
    const ws = socket
    if (!ws) {
      if (state.connection === 'connecting') {
        state.connection = 'idle'
        state.phase = 'idle'
      }
      return
    }
    socket = null
    detach(ws)
    try {
      if (ws.readyState === OPEN) ws.send(JSON.stringify({ type: 'hangup' }))
      ws.close()
    } catch {
      // already closing
    }
    if (state.connection === 'connecting') {
      state.connection = 'idle'
      state.phase = 'idle'
    } else if (state.connection === 'open') {
      state.connection = 'closed'
      state.ended = true
      if (state.endedAt === null) state.endedAt = now()
      state.phase = 'ended'
    }
  }

  // Back to the pre-call screen.
  function clear() {
    disconnect()
    reset()
    state.connection = 'idle'
  }

  return {
    start,
    sendText,
    ask,
    setMuted,
    setSite,
    hangup,
    disconnect,
    clear,
    get inCall() {
      return inCall()
    },
    get canSend() {
      return inCall()
    },
  }
}
