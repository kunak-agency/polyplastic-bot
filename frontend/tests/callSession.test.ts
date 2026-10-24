// The browser call (src/call/session.ts) against a fake WebSocket and a fake
// voice engine: what goes out on the socket, what lands in the transcript, and
// the barge-in bookkeeping of the voice protocol.
import { test } from 'node:test'
import assert from 'node:assert/strict'
import {
  createCallSession,
  createCallState,
  type CallEngine,
  type CallSocket,
  type EngineHandlers,
} from '../src/call/session.ts'

class FakeSocket implements CallSocket {
  readyState = 0
  binaryType: BinaryType = 'blob'
  onopen: ((ev: Event) => void) | null = null
  onmessage: ((ev: MessageEvent) => void) | null = null
  onerror: ((ev: Event) => void) | null = null
  onclose: ((ev: CloseEvent) => void) | null = null
  sent: (string | ArrayBuffer)[] = []
  closed = false

  send(data: string | ArrayBuffer) {
    this.sent.push(data)
  }
  close() {
    if (this.closed) return
    this.closed = true
    this.readyState = 3
    this.onclose?.({} as CloseEvent)
  }
  // Test side.
  open() {
    this.readyState = 1
    this.onopen?.({} as Event)
  }
  event(ev: Record<string, unknown>) {
    this.onmessage?.({ data: JSON.stringify(ev) } as MessageEvent)
  }
  binary(bytes = 320) {
    this.onmessage?.({ data: new ArrayBuffer(bytes) } as MessageEvent)
  }
  json(): Record<string, unknown>[] {
    return this.sent
      .filter((d): d is string => typeof d === 'string')
      .map((d) => JSON.parse(d) as Record<string, unknown>)
  }
  marks() {
    return this.json().filter((m) => m.type === 'playback.mark')
  }
}

class FakeEngine implements CallEngine {
  handlers: EngineHandlers
  played = 0
  cleared = 0
  marked: number[] = []
  muted = false
  closed = false
  failWith: unknown = null
  constructor(handlers: EngineHandlers) {
    this.handlers = handlers
  }
  open() {
    return this.failWith ? Promise.reject(this.failWith) : Promise.resolve()
  }
  setMuted(m: boolean) {
    this.muted = m
  }
  play() {
    this.played++
  }
  markEnd(turn: number) {
    this.marked.push(turn)
  }
  clear() {
    this.cleared++
  }
  closeAfterPlayback() {
    this.closed = true
    return Promise.resolve()
  }
  close() {
    this.closed = true
    return Promise.resolve()
  }
}

function setup(opts: { micError?: unknown } = {}) {
  const state = createCallState()
  const sockets: FakeSocket[] = []
  const engines: FakeEngine[] = []
  const session = createCallSession(state, {
    openSocket: () => {
      const s = new FakeSocket()
      sockets.push(s)
      return s
    },
    createEngine: (h) => {
      const e = new FakeEngine(h)
      if (opts.micError) e.failWith = opts.micError
      engines.push(e)
      return e
    },
    now: () => 1000,
  })
  return { state, session, sockets, engines }
}

const flush = () => new Promise<void>((r) => setImmediate(r))

// A voice call with the line open.
async function connected() {
  const ctx = setup()
  ctx.state.site = 'GTP'
  ctx.session.start()
  await flush()
  const ws = ctx.sockets[0]
  assert.ok(ws, 'socket opened after the mic went live')
  ws.open()
  return { ...ctx, ws, engine: ctx.engines[0] as FakeEngine }
}

test('start goes out first, once the socket opens, with the site', async () => {
  const { state, ws } = await connected()
  assert.equal(ws.binaryType, 'arraybuffer')
  assert.deepEqual(ws.json()[0], { type: 'start', input: 'voice', site: 'GTP' })
  assert.equal(state.connection, 'open')
  assert.equal(state.phase, 'listening')
  assert.equal(state.mic, 'live')
  assert.equal(state.startedAt, 1000)
})

test('mic frames are sent only after the line opens', async () => {
  const ctx = setup()
  ctx.session.start()
  await flush()
  const engine = ctx.engines[0] as FakeEngine
  const ws = ctx.sockets[0] as FakeSocket
  engine.handlers.onMicFrame(new ArrayBuffer(320))
  assert.equal(ws.sent.length, 0)
  ws.open()
  engine.handlers.onMicFrame(new ArrayBuffer(320))
  assert.equal(ws.sent.length, 2)
  assert.ok(ws.sent[1] instanceof ArrayBuffer)
})

test('partial is grey in the feed, then the final replaces it', async () => {
  const { state, ws } = await connected()
  ws.event({ type: 'user.speech', state: 'start' })
  ws.event({ type: 'user.partial', text: 'когда' })
  ws.event({ type: 'user.partial', text: 'когда аванс' })
  assert.equal(state.messages.length, 1)
  assert.deepEqual(
    { text: state.messages[0]?.text, partial: state.messages[0]?.partial },
    { text: 'когда аванс', partial: true },
  )
  assert.equal(state.userSpeaking, true)

  ws.event({ type: 'user.final', text: 'Когда аванс?' })
  assert.equal(state.messages.length, 1)
  assert.equal(state.messages[0]?.text, 'Когда аванс?')
  assert.equal(state.messages[0]?.partial, false)
  assert.equal(state.phase, 'thinking')
})

test('bot.say lands with its sources', async () => {
  const { state, ws } = await connected()
  ws.event({ type: 'user.final', text: 'Когда аванс?' })
  ws.event({ type: 'bot.say', turn: 1, text: 'Секунду, смотрю.', filler: true })
  ws.event({
    type: 'bot.say',
    turn: 1,
    text: 'Двадцать пятого числа.',
    sources: [{ doc: 'pvtr.pdf', clause_id: '6.7.4', page: 18, label: 'ПВТР 6.7.4' }],
  })
  const bot = state.messages.filter((m) => m.role === 'bot')
  assert.equal(bot.length, 2)
  assert.equal(bot[0]?.filler, true)
  assert.equal(bot[1]?.text, 'Двадцать пятого числа.')
  assert.deepEqual(
    bot[1]?.sources?.map((s) => s.label),
    ['ПВТР 6.7.4'],
  )
})

test('playback.clear drops buffered audio and frames of the old generation', async () => {
  const { ws, engine, state } = await connected()
  ws.event({ type: 'audio.start', turn: 1, gen: 1 })
  ws.binary()
  ws.binary()
  assert.equal(engine.played, 2)

  ws.event({ type: 'playback.clear', gen: 2 })
  assert.equal(engine.cleared, 1)
  // The open turn is reported as not played to the end.
  assert.deepEqual(ws.marks(), [{ type: 'playback.mark', turn: 1, complete: false }])
  assert.equal(state.phase, 'listening')

  // Late frames of the cleared generation are dropped.
  ws.binary()
  assert.equal(engine.played, 2)
  // A late audio.start of the old generation does not revive them.
  ws.event({ type: 'audio.start', turn: 1, gen: 1 })
  ws.binary()
  assert.equal(engine.played, 2)
  // The new generation plays.
  ws.event({ type: 'audio.start', turn: 2, gen: 2 })
  ws.binary()
  assert.equal(engine.played, 3)
})

test('playback.mark goes out once the audio up to audio.end has played', async () => {
  const { ws, engine, state } = await connected()
  ws.event({ type: 'audio.start', turn: 3, gen: 1 })
  ws.binary()
  engine.handlers.onPlaying(true)
  assert.equal(state.phase, 'speaking')
  ws.event({ type: 'audio.end', turn: 3 })
  // The end is marked in the player, not reported yet.
  assert.deepEqual(engine.marked, [3])
  assert.equal(ws.marks().length, 0)

  engine.handlers.onTurnPlayed(3)
  engine.handlers.onPlaying(false)
  assert.deepEqual(ws.marks(), [{ type: 'playback.mark', turn: 3, complete: true }])
  assert.equal(state.phase, 'listening')
  // A second report of the same turn is not sent twice.
  engine.handlers.onTurnPlayed(3)
  assert.equal(ws.marks().length, 1)
})

test('busy ends the call with the busy message, not a lost connection', async () => {
  const { ws, state, engine } = await connected()
  ws.event({ type: 'error', code: 'busy' })
  assert.equal(state.problem, 'busy')
  assert.equal(state.ended, true)
  assert.equal(engine.closed, true)
  ws.close()
  assert.equal(state.problem, 'busy')
  assert.equal(state.connection, 'closed')
  assert.equal(state.phase, 'ended')
})

test('hangup sends hangup, silences the engine and ends the call', async () => {
  const { ws, state, engine, session } = await connected()
  session.hangup()
  assert.deepEqual(ws.json().at(-1), { type: 'hangup' })
  assert.equal(engine.closed, true)
  assert.equal(state.ended, true)
  assert.equal(state.phase, 'ended')
  // The server answers call.end and closes: not a lost connection.
  ws.event({ type: 'call.end' })
  ws.close()
  assert.equal(state.problem, null)
  assert.equal(state.connection, 'closed')
})

test('a drop without call.end is a lost connection', async () => {
  const { ws, state } = await connected()
  ws.close()
  assert.equal(state.problem, 'lost')
  assert.equal(state.phase, 'ended')
})

test('mic denied: no socket, the reason is kept; text goes through a text call', async () => {
  const ctx = setup({ micError: { kind: 'denied' } })
  ctx.session.start()
  await flush()
  assert.equal(ctx.state.mic, 'denied')
  assert.equal(ctx.sockets.length, 0)
  assert.equal(ctx.state.connection, 'idle')

  ctx.session.ask('Когда выплачивают аванс?')
  const ws = ctx.sockets[0] as FakeSocket
  ws.open()
  assert.deepEqual(ws.json(), [
    { type: 'start', input: 'text', site: 'GrPP' },
    { type: 'user.text', text: 'Когда выплачивают аванс?' },
  ])
  assert.equal(ctx.state.messages[0]?.pending, true)
  ws.event({ type: 'user.final', text: 'Когда выплачивают аванс?' })
  assert.equal(ctx.state.messages.length, 1)
  assert.equal(ctx.state.messages[0]?.pending, false)
})
