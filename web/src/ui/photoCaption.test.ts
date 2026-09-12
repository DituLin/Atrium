import { describe, expect, it } from 'vitest';

import type { PhotoItem } from '../types/api';
import { captionText, photoCaption } from './photoCaption';

const options = { timezone: 'Asia/Singapore', utcOffsetSeconds: 28800 };
const item: PhotoItem = {
  id: 'photo', source_id: 'family', captured_at: '2026-09-05T16:07:00Z',
  captured_confidence: 'exact', first_seen_at: '2026-09-06T00:00:00Z',
  is_baseline: false, width: 100, height: 100,
  preview: { status: 'ready', width: 100, height: 100 },
  urls: { preview: '/preview', thumb: '/thumb' },
};

describe('Chinese photo captions', () => {
  it('retains home date and labels inferred timestamps explicitly', () => {
    expect(captionText(item, options)).toBe('照片，2026年9月6日 00:07');
    expect(captionText({ ...item, captured_confidence: 'inferred' }, options))
      .toBe('照片，2026年9月6日 00:07（估算）');
  });

  it('reports unknown capture time without using first-seen metadata', () => {
    const unknown = { ...item, captured_at: null, captured_confidence: 'unknown' as const };
    expect(captionText(unknown, options)).toBe('照片，拍摄时间未知');
    expect(photoCaption(unknown, options)).toEqual({ date: '', estimated: false });
    expect(captionText({ ...item, captured_at: 'invalid' }, options)).toBe('照片，拍摄时间未知');
  });
});
