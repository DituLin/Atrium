import { describe, expect, it } from 'vitest';
import { homeDateLine, homeLunarLine, homeStatus, photoFit } from './homeSummary';

const SG = { timezone: 'Asia/Singapore', utcOffsetSeconds: 8 * 3600 };
const at = (iso: string) => Date.parse(iso);

describe('homeSummary', () => {
  it('prints the family date and lunar date in the home timezone, rolling over at local midnight', () => {
    expect(homeDateLine(at('2026-09-26T13:24:00Z'), SG)).toBe('9月26日 · 星期六');
    expect(homeLunarLine(at('2026-09-26T13:24:00Z'), SG)).toBe('农历八月十六');
    expect(homeDateLine(at('2026-09-26T15:59:00Z'), SG)).toBe('9月26日 · 星期六');
    expect(homeDateLine(at('2026-09-26T16:00:00Z'), SG)).toBe('9月27日 · 星期日');
    expect(homeLunarLine(at('2026-09-26T16:00:00Z'), SG)).toBe('农历八月十七');
  });

  it('never reports unknown or disconnected states as healthy', () => {
    const src = (health: 'online' | 'offline' | 'degraded' | 'unknown') => ({ id: 's', name: 'S', health, last_check_at: null, last_success_at: null });
    expect(homeStatus('online', [src('online')])).toEqual({ tone: 'ok', text: '家中服务正常' });
    expect(homeStatus('reconnecting', [src('online')]).tone).toBe('warn');
    expect(homeStatus('online', [src('online'), src('offline')]).tone).toBe('warn');
    expect(homeStatus('online', [src('unknown')])).toEqual({ tone: 'none', text: '照片库状态待确认' });
    expect(homeStatus('online', []).tone).toBe('none');
  });

  it('fills the screen only with ordinary landscape photos', () => {
    expect(photoFit(4032, 3024)).toBe('cover');
    expect(photoFit(1920, 1080)).toBe('cover');
    expect(photoFit(3024, 4032)).toBe('contain');
    expect(photoFit(6000, 2000)).toBe('contain');
    expect(photoFit(0, 0)).toBe('contain');
  });
});
