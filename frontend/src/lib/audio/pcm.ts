// Copied from hack-ru-clinik frontend/src/lib/audio/pcm.ts (unchanged in substance).
// PCM helpers for the voice call (docs/call-protocol.md, voice mode section):
// the wire format is PCM16LE mono 8000 Hz in both directions.

export const WIRE_RATE = 8000
// 20 ms at 8 kHz.
export const FRAME_SAMPLES = 160

export function int16ToFloat32(pcm: Int16Array): Float32Array<ArrayBuffer> {
  const out = new Float32Array(pcm.length)
  for (let i = 0; i < pcm.length; i++) out[i] = (pcm[i] ?? 0) / 0x8000
  return out
}

// Little-endian Int16 view of a binary frame. Copies when the byte length is
// odd (malformed tail dropped) or the platform is big-endian (never, in practice).
export function frameToInt16(data: ArrayBuffer): Int16Array {
  return new Int16Array(data, 0, data.byteLength >> 1)
}

// Streaming linear-interpolation resampler 8 kHz → context rate, used only on
// the fallback path when the AudioContext could not run at 8 kHz. Keeps the
// last input sample and the fractional phase across frames, so frame borders
// do not click.
export class LinearUpsampler {
  private readonly step: number
  private pos = 0
  private prev = 0

  constructor(fromRate: number, toRate: number) {
    this.step = fromRate / toRate
  }

  reset() {
    this.pos = 0
    this.prev = 0
  }

  process(input: Float32Array): Float32Array<ArrayBuffer> {
    const n = input.length
    const at = (i: number) => (i < 0 ? this.prev : (input[i] ?? 0))
    const out: number[] = []
    // pos is relative to this block; index -1 is the previous block's last sample.
    while (this.pos <= n - 1) {
      const i = Math.floor(this.pos)
      const frac = this.pos - i
      const a = at(i)
      out.push(frac > 0 ? a + (at(i + 1) - a) * frac : a)
      this.pos += this.step
    }
    this.pos -= n
    if (n) this.prev = input[n - 1] ?? 0
    return Float32Array.from(out)
  }
}

// RMS (0..1) → meter level (0..1) on a -60…0 dBFS scale.
export function rmsToLevel(rms: number): number {
  if (rms <= 0) return 0
  const db = 20 * Math.log10(rms)
  return Math.min(1, Math.max(0, (db + 60) / 60))
}
