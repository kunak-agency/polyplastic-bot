// Copied from hack-ru-clinik frontend/src/lib/audio/voiceEngine.ts (unchanged in substance).
import captureWorkletUrl from './capture.worklet.ts?worker&url'
import playbackWorkletUrl from './playback.worklet.ts?worker&url'
import { frameToInt16, int16ToFloat32, LinearUpsampler, WIRE_RATE } from './pcm'

// Browser side of a voice call: one AudioContext (8 kHz when the browser allows
// it) for both the mic and the bot's speech — sharing it helps the browser's
// echo canceller — two AudioWorklets bundled as same-origin files (CSP:
// script-src 'self'), and nothing else. Transport-agnostic: the call store
// feeds it server frames and forwards its mic frames to the socket.

export type MicErrorKind = 'denied' | 'notFound' | 'unsupported' | 'failed'

export class MicError extends Error {
  readonly kind: MicErrorKind
  constructor(kind: MicErrorKind, cause?: unknown) {
    super(kind)
    this.kind = kind
    this.cause = cause
  }
}

export interface VoiceEngineHandlers {
  // 20 ms PCM16LE 8 kHz chunk from the mic; the buffer is owned by the callee.
  onMicFrame(pcm: ArrayBuffer): void
  // RMS of the latest chunk (0..1).
  onMicLevel(rms: number): void
  // Bot audio started / stopped coming out of the speakers.
  onPlaying(playing: boolean): void
  // Everything of `turn` up to its audio.end has been played.
  onTurnPlayed(turn: number): void
}

const CONSTRAINTS: MediaStreamConstraints = {
  audio: {
    echoCancellation: true,
    noiseSuppression: true,
    autoGainControl: true,
    channelCount: 1,
  },
  video: false,
}

function micError(e: unknown): MicError {
  const name = e instanceof DOMException ? e.name : ''
  if (name === 'NotAllowedError' || name === 'SecurityError') return new MicError('denied', e)
  if (name === 'NotFoundError' || name === 'OverconstrainedError')
    return new MicError('notFound', e)
  // No capture support in this context (headless, policy, insecure origin).
  if (name === 'NotSupportedError' || e instanceof TypeError) return new MicError('unsupported', e)
  return new MicError('failed', e)
}

function isNotSupported(e: unknown): boolean {
  return e instanceof DOMException && e.name === 'NotSupportedError'
}

export class VoiceEngine {
  private ctx: AudioContext | null = null
  private stream: MediaStream | null = null
  private source: MediaStreamAudioSourceNode | null = null
  private capture: AudioWorkletNode | null = null
  private player: AudioWorkletNode | null = null
  private upsampler: LinearUpsampler | null = null
  private closed = false
  private playing = false
  private closeTimer: ReturnType<typeof setTimeout> | null = null

  private readonly handlers: VoiceEngineHandlers

  constructor(handlers: VoiceEngineHandlers) {
    // A copy: closeAfterPlayback() wraps onPlaying without touching the caller's object.
    this.handlers = { ...handlers }
  }

  get sampleRate(): number {
    return this.ctx?.sampleRate ?? 0
  }

  // Must be called from a user gesture: the first AudioContext is created
  // before any await so autoplay policies treat it as user-initiated.
  async open(): Promise<void> {
    if (typeof AudioContext === 'undefined' || typeof AudioWorkletNode === 'undefined') {
      throw new MicError('unsupported')
    }
    if (!navigator.mediaDevices?.getUserMedia) throw new MicError('unsupported')

    let ctx: AudioContext | null = null
    try {
      ctx = new AudioContext({ sampleRate: WIRE_RATE })
    } catch (e) {
      if (!isNotSupported(e)) throw new MicError('failed', e)
    }
    this.ctx = ctx

    try {
      this.stream = await navigator.mediaDevices.getUserMedia(CONSTRAINTS)
    } catch (e) {
      await this.close()
      throw micError(e)
    }
    if (this.closed) return this.release()

    try {
      await this.build()
    } catch (e) {
      await this.close()
      throw e instanceof MicError ? e : new MicError('failed', e)
    }
  }

  private async build(): Promise<void> {
    let ctx = this.ctx
    let source: MediaStreamAudioSourceNode | null = null
    const stream = this.stream as MediaStream
    if (ctx) {
      try {
        source = ctx.createMediaStreamSource(stream)
      } catch (e) {
        // Firefox: an 8 kHz context cannot take a mic at the device rate.
        if (!isNotSupported(e)) throw e
        void ctx.close()
        ctx = null
      }
    }
    if (!ctx) {
      ctx = new AudioContext()
      source = ctx.createMediaStreamSource(stream)
    }
    this.ctx = ctx
    this.source = source
    if (ctx.state === 'suspended') await ctx.resume().catch(() => {})

    await ctx.audioWorklet.addModule(captureWorkletUrl)
    await ctx.audioWorklet.addModule(playbackWorkletUrl)
    if (this.closed) return

    if (Math.abs(ctx.sampleRate - WIRE_RATE) > 1) {
      this.upsampler = new LinearUpsampler(WIRE_RATE, ctx.sampleRate)
    }

    const capture = new AudioWorkletNode(ctx, 'pcm-capture', {
      numberOfInputs: 1,
      numberOfOutputs: 1,
      outputChannelCount: [1],
      channelCount: 1,
      channelCountMode: 'explicit',
    })
    capture.port.onmessage = (e: MessageEvent<{ pcm: ArrayBuffer; rms: number }>) => {
      if (this.closed) return
      this.handlers.onMicLevel(e.data.rms)
      this.handlers.onMicFrame(e.data.pcm)
    }
    // The capture node outputs silence; connecting it to the destination is
    // what keeps the graph pulling it in every browser.
    source?.connect(capture)
    capture.connect(ctx.destination)
    this.capture = capture

    const player = new AudioWorkletNode(ctx, 'pcm-player', {
      numberOfInputs: 0,
      numberOfOutputs: 1,
      outputChannelCount: [1],
    })
    player.port.onmessage = (
      e: MessageEvent<{ type: 'playing'; playing: boolean } | { type: 'marked'; turn: number }>,
    ) => {
      if (this.closed) return
      const msg = e.data
      if (msg.type === 'playing') {
        this.playing = msg.playing
        this.handlers.onPlaying(msg.playing)
      } else this.handlers.onTurnPlayed(msg.turn)
    }
    player.connect(ctx.destination)
    this.player = player
  }

  get isPlaying(): boolean {
    return this.playing
  }

  setMuted(muted: boolean) {
    // A disabled track yields silence: frames keep flowing, so the server's
    // stream stays continuous, but they carry nothing.
    for (const track of this.stream?.getAudioTracks() ?? []) track.enabled = !muted
  }

  // One binary frame of bot speech (PCM16LE 8 kHz).
  play(data: ArrayBuffer) {
    if (!this.player || data.byteLength < 2) return
    let samples = int16ToFloat32(frameToInt16(data))
    if (this.upsampler) samples = this.upsampler.process(samples)
    this.player.port.postMessage({ type: 'push', samples }, [samples.buffer])
  }

  // audio.end: the turn's audio ends at the current write position.
  markEnd(turn: number) {
    this.player?.port.postMessage({ type: 'mark', turn })
  }

  // playback.clear: drop everything buffered right now.
  clear() {
    this.upsampler?.reset()
    this.player?.port.postMessage({ type: 'clear' })
  }

  // Stops the mic now; lets buffered bot speech play out (up to `maxMs`), then
  // closes. Used when the call ends on the server's side.
  async closeAfterPlayback(maxMs = 15000): Promise<void> {
    this.stopMic()
    if (!this.playing || this.closed) return this.close()
    await new Promise<void>((resolve) => {
      const prev = this.handlers.onPlaying
      this.handlers.onPlaying = (playing) => {
        prev(playing)
        if (!playing) resolve()
      }
      this.closeTimer = setTimeout(resolve, maxMs)
    })
    return this.close()
  }

  private stopMic() {
    for (const track of this.stream?.getTracks() ?? []) track.stop()
    this.source?.disconnect()
    this.capture?.disconnect()
    if (this.capture) this.capture.port.onmessage = null
    this.source = null
    this.capture = null
  }

  private release() {
    this.stopMic()
    this.stream = null
  }

  async close(): Promise<void> {
    if (this.closeTimer) clearTimeout(this.closeTimer)
    this.closeTimer = null
    this.closed = true
    this.release()
    if (this.player) {
      this.player.port.onmessage = null
      this.player.disconnect()
      this.player = null
    }
    if (this.playing) {
      this.playing = false
      this.handlers.onPlaying(false)
    }
    const ctx = this.ctx
    this.ctx = null
    if (ctx && ctx.state !== 'closed') await ctx.close().catch(() => {})
  }
}
