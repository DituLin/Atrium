/**
 * Pure text for the ambient home screen, kept out of the component so the
 * wording and the honesty rules can be asserted directly.
 */

import type { ClockFormatOptions } from '../../core/clock';
import { wallClockParts } from '../../core/clock';
import { almanac } from '../../core/lunar';
import type { NasSourceStatus, OverviewResponse } from '../../types/api';
import { projectOverview } from '../../app/overview';
import { worstHealth } from '../../ui/statusText';

const WEEKDAYS = ['星期日', '星期一', '星期二', '星期三', '星期四', '星期五', '星期六'];

/** `9月26日 · 星期六` in the home timezone. */
export function homeDateLine(epochMs: number, options: ClockFormatOptions): string {
  const parts = wallClockParts(epochMs, options);
  return `${parts.month}月${parts.day}日 · ${WEEKDAYS[parts.weekday] ?? ''}`;
}

/** `农历八月十六`, or null outside the lunar table. */
export function homeLunarLine(epochMs: number, options: ClockFormatOptions): string | null {
  const parts = wallClockParts(epochMs, options);
  const day = almanac(parts.year, parts.month, parts.day);
  return day ? `农历${day.lunar}` : null;
}

export type HomeTone = 'ok' | 'warn' | 'none';

/**
 * One line of household status. Normal stays quiet; anything else says what is
 * wrong in words. Unknown is not reported as healthy.
 */
export function homeStatus(connection: string, sources: readonly NasSourceStatus[]): { tone: HomeTone; text: string } {
  if (connection !== 'online') return { tone: 'warn', text: '正在连接家里的服务' };
  if (sources.length === 0) return { tone: 'none', text: '照片库尚未接入' };
  switch (worstHealth(sources)) {
    case 'online': return { tone: 'ok', text: '家中服务正常' };
    case 'offline': return { tone: 'warn', text: '照片库离线，显示已缓存的照片' };
    case 'degraded': return { tone: 'warn', text: '照片库读取异常' };
    default: return { tone: 'none', text: '照片库状态待确认' };
  }
}

/** Currently valid family notices, newest projection order; never expired ones. */
export function homeNotices(snapshot: OverviewResponse | null, nowMs: number, stale: boolean): string[] {
  // A malformed or partial snapshot shows no notice rather than breaking home.
  if (!snapshot?.sources?.notice) return [];
  const projected = projectOverview(snapshot.sources, nowMs, stale);
  const notice = projected.sources.notice;
  const texts: string[] = [];
  for (const entry of projected.entries) {
    if (entry.kind !== 'notice') continue;
    const item = notice.items.find(candidate => candidate.id === entry.item_id);
    if (item) texts.push(item.text);
  }
  return texts;
}

/**
 * Landscape photos fill the screen; portraits and panoramas are shown whole on
 * ink so faces and edges are never cropped away.
 */
export function photoFit(width: number, height: number): 'cover' | 'contain' {
  if (!width || !height) return 'contain';
  const ratio = width / height;
  return ratio >= 1.25 && ratio <= 2.1 ? 'cover' : 'contain';
}
