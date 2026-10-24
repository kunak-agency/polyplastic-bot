// WebSocket /api/call, the browser call to Korsik (call.plexet.ru). A subset of
// the hack-ru-clinik protocol (docs/call-protocol.md, voice mode section) plus
// the site of the caller and the regulation sources of an answer.
//
// One socket per call; every text frame is JSON `{ type, ... }`. Binary frames
// are PCM16LE mono audio (mic → server 8 kHz, server → browser 24 kHz) and carry no type.

export type CallSite = 'GrPP' | 'GTP' | 'Luzhniki'
export const CALL_SITES: readonly CallSite[] = ['GrPP', 'GTP', 'Luzhniki']

// `text` is the fallback when the mic is unavailable: no audio either way.
export type CallInput = 'voice' | 'text'

// ── Client → server ─────────────────────────────────────────────────────────

export interface StartMessage {
  type: 'start'
  input: CallInput
  site: CallSite | null
}

export interface UserTextMessage {
  type: 'user.text'
  text: string
}

export interface PlaybackMarkMessage {
  type: 'playback.mark'
  turn: number
  complete: boolean
}

export interface HangupMessage {
  type: 'hangup'
}

export type CallClientMessage = StartMessage | UserTextMessage | PlaybackMarkMessage | HangupMessage

// ── Server → client ─────────────────────────────────────────────────────────

export interface CallSource {
  doc: string
  clause_id: string
  page: number | null
  // Short caption for the chip, e.g. a regulation abbreviation and a clause.
  label: string
}

export interface CallStartedEvent {
  type: 'call.started'
  callId?: string
}

export interface BotDeltaEvent {
  type: 'bot.delta'
  turn: number
  text: string
}

export interface BotSayEvent {
  type: 'bot.say'
  text: string
  turn?: number
  sources?: CallSource[]
  // A stalling phrase while the answer is being looked up.
  filler?: boolean
}

export interface UserPartialEvent {
  type: 'user.partial'
  text: string
}

export interface UserFinalEvent {
  type: 'user.final'
  text: string
}

// Server-side VAD. Tolerated variant: `{text}` without `state` is a final.
export interface UserSpeechEvent {
  type: 'user.speech'
  state?: 'start' | 'end'
  text?: string
}

export interface AudioStartEvent {
  type: 'audio.start'
  turn: number
  gen: number
}

export interface AudioEndEvent {
  type: 'audio.end'
  turn: number
}

export interface PlaybackClearEvent {
  type: 'playback.clear'
  gen: number
}

export interface TurnMetricsEvent {
  type: 'turn.metrics'
  turn?: number
  [metric: string]: unknown
}

export interface CallEndEvent {
  type: 'call.end'
  reason?: string
  outcome?: string
}

// `busy`: no free line (concurrent call limit). Other codes are shown generically.
export interface CallErrorEvent {
  type: 'error'
  code: string
  message?: string
}

export type CallServerEvent =
  | CallStartedEvent
  | BotDeltaEvent
  | BotSayEvent
  | UserPartialEvent
  | UserFinalEvent
  | UserSpeechEvent
  | AudioStartEvent
  | AudioEndEvent
  | PlaybackClearEvent
  | TurnMetricsEvent
  | CallEndEvent
  | CallErrorEvent

// ── Screen state ────────────────────────────────────────────────────────────

export type CallConnection = 'idle' | 'connecting' | 'open' | 'closed' | 'failed'

export type CallPhase = 'idle' | 'connecting' | 'listening' | 'thinking' | 'speaking' | 'ended'

export type MicState =
  'off' | 'starting' | 'live' | 'denied' | 'notFound' | 'unsupported' | 'failed'

// What went wrong, for the error line: a server code, or the connection.
export type CallProblem = 'busy' | 'lost' | 'failed' | 'server'

export interface CallMessage {
  id: number
  role: 'user' | 'bot'
  text: string
  // Interim recognition, greyed until the final replaces it.
  partial?: boolean
  // Typed text sent, its echo not yet received.
  pending?: boolean
  filler?: boolean
  streaming?: boolean
  turn?: number
  sources?: CallSource[]
}
