import { describe, expect, it } from 'vitest';

import {
  TIME_UNVERIFIED_AFTER_MS,
  clockReducer,
  formatClock,
  homeDayKey,
  initialClockState,
  isTimeUnverified,
  serverNow,
  supportsTimeZone,
} from './clock';

const SG = { timezone: 'Asia/Singapore', utcOffsetSeconds: 8 * 3600 };

describe('clock offset', () => {
  it('derives the offset from a server timestamp', () => {
    const state = clockReducer(initialClockState, {
      type: 'clock.sync',
      serverTime: '2026-09-05T00:00:10.000Z',
      receivedAt: Date.parse('2026-09-05T00:00:00.000Z'),
    });
    expect(state.offsetMs).toBe(10_000);
    expect(serverNow(state, Date.parse('2026-09-05T00:00:00.000Z'))).toBe(
      Date.parse('2026-09-05T00:00:10.000Z'),
    );
  });

  it('ignores an unparseable server time', () => {
    const state = clockReducer(initialClockState, {
      type: 'clock.sync',
      serverTime: 'not a time',
      receivedAt: 1000,
    });
    expect(state).toBe(initialClockState);
  });

  it('marks the time unverified after 6 h without a sync', () => {
    const synced = clockReducer(initialClockState, {
      type: 'clock.sync',
      serverTime: '2026-09-05T00:00:00.000Z',
      receivedAt: 0,
    });
    expect(isTimeUnverified(synced, TIME_UNVERIFIED_AFTER_MS)).toBe(false);
    expect(isTimeUnverified(synced, TIME_UNVERIFIED_AFTER_MS + 1)).toBe(true);
    expect(isTimeUnverified(initialClockState, 0)).toBe(true);
  });
});

describe('formatting with a named timezone', () => {
  it('is available in this engine', () => {
    expect(supportsTimeZone('Asia/Singapore')).toBe(true);
  });

  it('renders the home wall clock', () => {
    const epoch = Date.parse('2026-09-05T02:07:09.000Z'); // 10:07:09 +08
    const out = formatClock(epoch, { ...SG, timeZoneSupported: true });
    expect(out.time).toBe('10:07');
    expect(out.seconds).toBe('09');
    expect(out.date).toBe('Saturday, 5 September 2026');
    expect(out.dayKey).toBe('2026-09-05');
  });

  it('rolls the date over at home midnight, not device midnight', () => {
    const before = Date.parse('2026-09-05T15:59:59.000Z'); // 23:59:59 +08
    const after = Date.parse('2026-09-05T16:00:00.000Z'); // 00:00:00 +08 next day
    expect(homeDayKey(before, { ...SG, timeZoneSupported: true })).toBe('2026-09-05');
    expect(homeDayKey(after, { ...SG, timeZoneSupported: true })).toBe('2026-09-06');
  });
});

describe('formatting without Intl timezone support', () => {
  it('falls back to utc_offset_seconds and matches the Intl result', () => {
    const epoch = Date.parse('2026-09-05T02:07:09.000Z');
    const withIntl = formatClock(epoch, { ...SG, timeZoneSupported: true });
    const withoutIntl = formatClock(epoch, { ...SG, timeZoneSupported: false });
    expect(withoutIntl.time).toBe(withIntl.time);
    expect(withoutIntl.date).toBe(withIntl.date);
    expect(withoutIntl.dayKey).toBe(withIntl.dayKey);
  });

  it('still rolls over at the offset midnight', () => {
    const after = Date.parse('2026-09-05T16:00:00.000Z');
    expect(homeDayKey(after, { ...SG, timeZoneSupported: false })).toBe('2026-09-06');
  });
});
