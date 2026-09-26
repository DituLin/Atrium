/**
 * The player header and info line, from what the API actually returned: a
 * position only when the video is in the loaded list, a total only when the
 * list is complete, and format facts only when metadata exists.
 */

import type { VideoItem } from '../../types/api';

export interface VideoFacts {
  title?: string;
  subtitle?: string;
  info?: string;
}

const CODECS: Readonly<Record<string, string>> = { h264: 'H.264', avc: 'H.264', avc1: 'H.264', hevc: 'H.265', h265: 'H.265' };

/** `index` is the position in the loaded list (-1 when absent); `total` is null while more pages exist. */
export function videoFacts(detail: VideoItem | null, index: number, total: number | null): VideoFacts {
  const facts: VideoFacts = {};
  if (index >= 0) {
    facts.title = `影像 ${String(index + 1).padStart(2, '0')}`;
    facts.subtitle = total !== null && total > index ? `视频 · 第 ${index + 1} / ${total} 段` : `视频 · 第 ${index + 1} 段`;
  }
  const metadata = detail?.metadata;
  if (metadata && metadata.width > 0 && metadata.height > 0) {
    const codec = CODECS[metadata.video_codec.toLowerCase()] ?? metadata.video_codec.toUpperCase();
    facts.info = `${Math.min(metadata.width, metadata.height)}p · ${codec}`;
  }
  return facts;
}
