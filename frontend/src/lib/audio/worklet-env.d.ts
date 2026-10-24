// Copied from hack-ru-clinik frontend/src/lib/audio/worklet-env.d.ts (unchanged in substance).
// Ambient declarations for the AudioWorkletGlobalScope. lib.dom does not ship
// them; only the *.worklet.ts files in this folder run in that scope.

declare const sampleRate: number

declare class AudioWorkletProcessor {
  readonly port: MessagePort
  constructor(options?: AudioWorkletNodeOptions)
}

declare function registerProcessor(
  name: string,
  processorCtor: new (options: AudioWorkletNodeOptions) => AudioWorkletProcessor,
): void
