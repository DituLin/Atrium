/**
 * Family-local dates and times for the information pages. Every date is taken
 * in the home timezone from server time; when the zone cannot be trusted the
 * pages print no date rather than an invented one.
 */

import type { ClockFormatOptions } from '../../core/clock';
import { supportsTimeZone, wallClockParts } from '../../core/clock';
import { clockWidget } from '../../app/homeSelect';
import type { FamilyAvailability, HomeResponse } from '../../types/api';

const WEEKDAYS = ['星期日', '星期一', '星期二', '星期三', '星期四', '星期五', '星期六'];

/**
 * The clock widget carries a fixed offset fallback, so it always yields a
 * family date. A bare zone name (home header or family snapshot) is only used
 * when this engine understands it.
 */
export function familyClockOptions(home: HomeResponse | null, snapshotTimezone: string | null | undefined): ClockFormatOptions | null {
  const clock = clockWidget(home);
  if (clock) return { timezone: clock.timezone, utcOffsetSeconds: clock.utc_offset_seconds };
  const timezone = home?.home.timezone ?? snapshotTimezone;
  return timezone && supportsTimeZone(timezone) ? { timezone, utcOffsetSeconds: 0 } : null;
}

export interface FamilyDay {
  year: number;
  month: number;
  day: number;
  weekday: string;
  /** 9月26日 · 星期六 */
  label: string;
}

export function familyDay(epochMs: number, options: ClockFormatOptions | null): FamilyDay | null {
  if (!options) return null;
  const parts = wallClockParts(epochMs, options);
  if (!parts.year || !parts.month || !parts.day) return null;
  const weekday = WEEKDAYS[parts.weekday] ?? '';
  return { year: parts.year, month: parts.month, day: parts.day, weekday, label: `${parts.month}月${parts.day}日 · ${weekday}` };
}

/** `9月27日 12:00` (optionally with seconds) in the family zone. */
export function familyTime(value: string | null, timezone: string, seconds = false): string {
  if (!value) return '尚无记录';
  try {
    return new Intl.DateTimeFormat('zh-CN', {
      timeZone: timezone, month: 'long', day: 'numeric', hour: '2-digit', minute: '2-digit',
      second: seconds ? '2-digit' : undefined, hour12: false,
    }).format(new Date(value));
  } catch { return '时间待确认'; }
}

/** `21:24` or `21:24:05` in the family zone, for "汇总于 / 更新于" asides. */
export function familyClockTime(value: string | null, timezone: string, seconds = false): string | null {
  if (!value) return null;
  try {
    return new Intl.DateTimeFormat('zh-CN', {
      timeZone: timezone, hour: '2-digit', minute: '2-digit', second: seconds ? '2-digit' : undefined, hour12: false,
    }).format(new Date(value));
  } catch { return null; }
}

export type Tone = 'ok' | 'warn' | 'none';

/** Hollow for "not there (yet)", ochre for "should be there but is not current". */
export function availabilityTone(availability: FamilyAvailability): Tone {
  if (availability === 'available') return 'ok';
  return availability === 'stale' || availability === 'failed' ? 'warn' : 'none';
}
