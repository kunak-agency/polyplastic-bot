// Copied from hack-ru-clinik frontend/src/lib/audio/playback.worklet.ts (unchanged in substance).
// Bot speech player ('pcm-player'), runs in the AudioWorkletGlobalScope.
// A Float32 ring buffer (2 s, grows if the server sends faster than real time)
// fed from the main thread with samples already at the context rate.
//
// Commands (port messages from the main thread):
//   { type: 'push', samples: Float32Array }  append audio
//   { type: 'mark', turn }                    the turn's audio ends here (audio.end)
//   { type: 'clear' }                         barge-in: drop everything now
// Reports:
//   { type: 'playing', playing }              output started / stopped
//   { type: 'marked', turn }                  playback passed the turn's mark:
//                                             everything of that turn was played
//
// Playback starts only once ~60 ms is buffered (or a mark is pending, so a
// short tail is not held back), and re-primes after an underrun.

const PREFETCH_S = 0.06
const INITIAL_S = 2
const MAX_S = 120

interface Mark {
  // Absolute sample index (in written-sample count) where the turn ends.
  at: number
  turn: number
}

type Command =
  { type: 'push'; samples: Float32Array } | { type: 'mark'; turn: number } | { type: 'clear' }

class PcmPlayerProcessor extends AudioWorkletProcessor {
  private buf = new Float32Array(Math.round(sampleRate * INITIAL_S))
  // Monotonic counters of samples written / read; their difference is the fill.
  private written = 0
  private read = 0
  private marks: Mark[] = []
  private playing = false
  private readonly prefetch = Math.round(sampleRate * PREFETCH_S)

  constructor(options: AudioWorkletNodeOptions) {
    super(options)
    this.port.onmessage = (e: MessageEvent<Command>) => this.command(e.data)
  }

  private get fill(): number {
    return this.written - this.read
  }

  private command(cmd: Command) {
    if (cmd.type === 'push') this.push(cmd.samples)
    else if (cmd.type === 'mark') {
      this.marks.push({ at: this.written, turn: cmd.turn })
      this.flushMarks()
    } else if (cmd.type === 'clear') {
      this.read = this.written
      this.marks = []
      this.setPlaying(false)
    }
  }

  private grow(need: number) {
    let size = this.buf.length
    while (size < need && size < sampleRate * MAX_S) size *= 2
    if (size === this.buf.length) return
    const next = new Float32Array(size)
    for (let i = this.read; i < this.written; i++) {
      next[i % size] = this.buf[i % this.buf.length] ?? 0
    }
    this.buf = next
  }

  private push(samples: Float32Array) {
    if (this.fill + samples.length > this.buf.length) this.grow(this.fill + samples.length)
    const cap = this.buf.length
    // Still over capacity at the hard limit: drop the oldest audio.
    const overflow = this.fill + samples.length - cap
    if (overflow > 0) this.read += overflow
    for (let i = 0; i < samples.length; i++) {
      this.buf[(this.written + i) % cap] = samples[i] ?? 0
    }
    this.written += samples.length
  }

  // Marks at or behind the read position are done.
  private flushMarks() {
    while (this.marks.length && (this.marks[0]?.at ?? Infinity) <= this.read) {
      const mark = this.marks.shift()
      if (mark) this.port.postMessage({ type: 'marked', turn: mark.turn })
    }
  }

  private setPlaying(playing: boolean) {
    if (playing === this.playing) return
    this.playing = playing
    this.port.postMessage({ type: 'playing', playing })
  }

  process(_inputs: Float32Array[][], outputs: Float32Array[][]): boolean {
    const out = outputs[0]?.[0]
    if (!out) return true
    if (!this.playing && this.fill > 0 && (this.fill >= this.prefetch || this.marks.length)) {
      this.setPlaying(true)
    }
    if (this.playing) {
      const cap = this.buf.length
      const n = Math.min(out.length, this.fill)
      for (let i = 0; i < n; i++) out[i] = this.buf[(this.read + i) % cap] ?? 0
      this.read += n
      // The rest of `out` stays zero (the engine pre-fills outputs with silence).
      this.flushMarks()
      if (this.fill === 0) this.setPlaying(false)
    } else {
      this.flushMarks()
    }
    return true
  }
}

registerProcessor('pcm-player', PcmPlayerProcessor)

export {}
