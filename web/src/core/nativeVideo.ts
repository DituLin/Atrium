/**
 * Native playback offered by the Android TV host (`AtriumNative`). The TV's
 * WebView cannot decode HEVC, most family videos; the host's ExoPlayer can.
 * Outside that host (browsers, tests) this returns null and the web player is
 * used unchanged.
 */

export interface NativeVideoBridge {
  play(path: string, title: string, subtitle: string): void;
  stop(): void;
}

/** Fired on `window` by the host when native playback closes. */
export const NATIVE_VIDEO_EVENT = 'atrium-native-video';

export interface NativeVideoResult {
  positionMs: number;
  durationMs: number;
  ended: boolean;
  /** ExoPlayer error code name, or null. */
  error: string | null;
}

export function nativeVideo(): NativeVideoBridge | null {
  const bridge = (window as unknown as { AtriumNative?: Partial<NativeVideoBridge> }).AtriumNative;
  return bridge && typeof bridge.play === 'function' && typeof bridge.stop === 'function'
    ? (bridge as NativeVideoBridge)
    : null;
}

/** Decoder failures mean this device cannot play the format at all. */
export function isDecoderError(error: string | null): boolean {
  return !!error && /DECOD/.test(error);
}
