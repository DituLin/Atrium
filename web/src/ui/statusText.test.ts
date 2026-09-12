import { describe, expect, it } from 'vitest';

import type { NasSourceStatus, PhotoWidgetPayload } from '../types/api';
import {
  formatBytes,
  indexProgressText,
  nasStatusText,
  relativeTime,
  shareFreeText,
  worstHealth,
} from './statusText';

const NOW = Date.parse('2026-09-06T12:00:00Z');

function source(over: Partial<NasSourceStatus> = {}): NasSourceStatus {
  return {
    id: 'family_photos',
    name: 'Family photos',
    health: 'online',
    last_check_at: '2026-09-06T11:58:00Z',
    last_success_at: '2026-09-06T11:58:00Z',
    ...over,
  };
}

function photoWidget(over: Partial<PhotoWidgetPayload> = {}): PhotoWidgetPayload {
  return {
    slideshow_interval_seconds: 30,
    totals: { ready: 10, pending_preview: 0, unsupported: 0 },
    new_today: 0,
    captured_today: 0,
    unknown_captured: 0,
    baseline: { status: 'done', completed_at: null },
    index: {
      state: 'idle',
      progress: { seen: 10, indexed: 10, pending_preview: 0 },
      last_scan_at: null,
    },
    ...over,
  };
}

describe('status text: NAS (FR-07)', () => {
  it('reports the worst health of all sources', () => {
    expect(worstHealth([source(), source({ health: 'degraded' })])).toBe('degraded');
    expect(worstHealth([source({ health: 'degraded' }), source({ health: 'offline' })])).toBe(
      'offline',
    );
    expect(worstHealth([])).toBe('unknown');
  });

  it('names the state in words and says when it was last checked', () => {
    expect(nasStatusText([source()], NOW)).toEqual({
      health: 'online',
      value: '在线',
      detail: '上次检查：2 分钟前',
    });
    expect(nasStatusText([source({ health: 'offline', last_check_at: null })], NOW)).toEqual({
      health: 'offline',
      value: '离线',
      detail: '检查时间未知',
    });
    expect(nasStatusText([], NOW)).toEqual({
      health: 'unknown',
      value: '未知',
      detail: '未配置照片来源',
    });
  });

  it('names the affected source when several are configured', () => {
    const text = nasStatusText(
      [source(), source({ id: 'archive', name: 'Archive', health: 'degraded' })],
      NOW,
    );
    expect(text.detail).toBe('Archive · 上次检查：2 分钟前');
  });

  it('formats ages in the units a glance can read', () => {
    expect(relativeTime('2026-09-06T11:59:40Z', NOW)).toBe('刚刚');
    expect(relativeTime('2026-09-06T09:00:00Z', NOW)).toBe('3 小时前');
    expect(relativeTime('2026-09-04T09:00:00Z', NOW)).toBe('2 天前');
    expect(relativeTime('2026-09-06T11:58:00Z', NOW)).toBe('2 分钟前');
    expect(relativeTime(null, NOW)).toBe('时间未知');
    expect(relativeTime('not a date', NOW)).toBe('时间未知');
  });
});

describe('status text: share free space (FR-12)', () => {
  it('shows free space only when the server reports it', () => {
    expect(shareFreeText([source({ share_free_bytes: 512 * 1024 ** 3 })])).toBe('剩余 512 GB');
    expect(shareFreeText([source()])).toBeNull();
    expect(shareFreeText([source({ share_free_bytes: null })])).toBeNull();
  });

  it('formats bytes with one decimal below 100 units', () => {
    expect(formatBytes(0)).toBe('0 B');
    expect(formatBytes(1536)).toBe('1.5 KB');
    expect(formatBytes(4 * 1024 ** 4)).toBe('4 TB');
  });
});

describe('status text: index progress (W-204)', () => {
  it('shows the baseline import percentage', () => {
    const text = indexProgressText(
      photoWidget({
        baseline: { status: 'importing', completed_at: null },
        index: {
          state: 'baseline_import',
          progress: { seen: 200, indexed: 50, pending_preview: 12 },
          last_scan_at: null,
        },
      }),
    );
    expect(text).toBe('首次导入 25%（50 / 200）');
  });

  it('shows a scan and nothing at all when idle', () => {
    expect(
      indexProgressText(
        photoWidget({
          index: {
            state: 'scanning',
            progress: { seen: 10, indexed: 7, pending_preview: 0 },
            last_scan_at: null,
          },
        }),
      ),
    ).toBe('扫描中 · 已索引 7 项');
    expect(indexProgressText(photoWidget())).toBeNull();
    expect(indexProgressText(null)).toBeNull();
  });
});
