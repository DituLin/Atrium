import { expect, it } from 'vitest';

import type { VideoItem } from '../../types/api';
import { videoFacts } from './videoFacts';

const video: VideoItem = { id: 'v', source_id: 's', revision: 1, status: 'ready', first_seen_at: '2026-09-14T00:00:00Z',
  metadata: { container: 'mp4', video_codec: 'h264', audio_codec: 'aac', width: 1920, height: 1080, duration_ms: 225000, rotation: 0 } };

it('reports only what the list and metadata know', () => {
  expect(videoFacts(video, 1, 412)).toEqual({ title: '影像 02', subtitle: '视频 · 第 2 / 412 段', info: '1080p · H.264' });
  expect(videoFacts(video, 1, null)).toEqual({ title: '影像 02', subtitle: '视频 · 第 2 段', info: '1080p · H.264' });
  expect(videoFacts({ ...video, metadata: undefined }, -1, 3)).toEqual({});
});
