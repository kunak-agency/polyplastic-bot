// Copied from hack-ru-clinik frontend/src/lib/audio/capture.worklet.ts (unchanged in substance).
// Mic capture processor ('pcm-capture'), runs in the AudioWorkletGlobalScope.
// Input: the mic at the context rate. Output to the main thread: 20 ms chunks of
// PCM16LE mono 8 kHz (160 samples) as transferred ArrayBuffers, plus the RMS of
// each chunk for the level meter. When the context does not run at 8 kHz
// (Firefox rejects an 8 kHz context fed by a mic), the signal is low-passed
// (two cascaded Butterworth biquads, 4th order, cutoff 3.4 kHz) and decimated
// with linear interpolation.

const TARGET_RATE = 8000
const CHUNK = 160 // 20 ms at 8 kHz

class Biquad {
  private b0: number
  private b1: number
  private b2: number
  private a1: number
  private a2: number
  private z1 = 0
  private z2 = 0

  constructor(rate: number, cutoff: number, q: number) {
    const w0 = (2 * Math.PI * cutoff) / rate
    const alpha = Math.sin(w0) / (2 * q)
    const cos = Math.cos(w0)
    const a0 = 1 + alpha
    this.b0 = (1 - cos) / 2 / a0
    this.b1 = (1 - cos) / a0
    this.b2 = (1 - cos) / 2 / a0
    this.a1 = (-2 * cos) / a0
    this.a2 = (1 - alpha) / a0
  }

  // Transposed direct form II.
  step(x: number): number {
    const y = this.b0 * x + this.z1
    this.z1 = this.b1 * x - this.a1 * y + this.z2
    this.z2 = this.b2 * x - this.a2 * y
    return y
  }
}

class PcmCaptureProcessor extends AudioWorkletProcessor {
  private readonly ratio = sampleRate / TARGET_RATE
  private readonly resample = Math.abs(sampleRate - TARGET_RATE) > 1
  // Butterworth 4th order = two biquads with Q 0.5412 and 1.3066.
  private readonly lp1 = new Biquad(sampleRate, 3400, 0.5412)
  private readonly lp2 = new Biquad(sampleRate, 3400, 1.3066)
  // Fractional read position (in input samples) of the next output sample,
  // relative to the current input block; it may be in [-1, 0), where index -1
  // is `prev`, the last filtered sample of the previous block.
  private pos = 0
  private prev = 0
  private chunk = new Int16Array(CHUNK)
  private filled = 0
  private sumSq = 0

  private emit(v: number) {
    const s = v < -1 ? -1 : v > 1 ? 1 : v
    this.sumSq += s * s
    this.chunk[this.filled++] = s < 0 ? s * 0x8000 : s * 0x7fff
    if (this.filled === CHUNK) {
      const rms = Math.sqrt(this.sumSq / CHUNK)
      const buf = this.chunk.buffer
      this.port.postMessage({ pcm: buf, rms }, [buf])
      this.chunk = new Int16Array(CHUNK)
      this.filled = 0
      this.sumSq = 0
    }
  }

  process(inputs: Float32Array[][]): boolean {
    const channel = inputs[0]?.[0]
    if (!channel) return true
    if (!this.resample) {
      for (let i = 0; i < channel.length; i++) this.emit(channel[i] ?? 0)
      return true
    }
    const filtered = new Float32Array(channel.length)
    for (let i = 0; i < channel.length; i++) {
      filtered[i] = this.lp2.step(this.lp1.step(channel[i] ?? 0))
    }
    const n = filtered.length
    // x(-1) is the last filtered sample of the previous block.
    const at = (i: number) => (i < 0 ? this.prev : (filtered[i] ?? 0))
    while (this.pos <= n - 1) {
      const i = Math.floor(this.pos)
      const frac = this.pos - i
      const a = at(i)
      this.emit(frac > 0 ? a + (at(i + 1) - a) * frac : a)
      this.pos += this.ratio
    }
    this.pos -= n
    this.prev = filtered[n - 1] ?? 0
    return true
  }
}

registerProcessor('pcm-capture', PcmCaptureProcessor)

export {}
