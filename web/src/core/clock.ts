/**
 * Clock (design §7.2, FR-02).
 *
 * The server owns the time. Every `/home` response and every `heartbeat.ack`
 * carries `server_time`; from it we derive an offset and then tick locally each
 * second. Formatting uses `Intl.DateTimeFormat` with the home timezone when the
 * engine supports named zones, otherwise the fixed `utc_offset_seconds` from
 * the clock widget. When no server time has arrived for 6 h the UI shows a
 * "time unverified" badge.
 */

export const TIME_UNVERIFIED_AFTER_MS = 6 * 60 * 60 * 1000;

export interface ClockState {
  /** serverTime - clientTime at the last sync, in milliseconds. */
  offsetMs: number;
  /** Client epoch of the last successful sync, null when never synced. */
  lastSyncAt: number | null;
}

export const initialClockState: ClockState = { offsetMs: 0, lastSyncAt: null };

export type ClockAction =
  | { type: 'clock.sync'; serverTime: string; receivedAt: number }
  | { type: 'clock.reset' };

export function clockReducer(state: ClockState, action: ClockAction): ClockState {
  switch (action.type) {
    case 'clock.sync': {
      const parsed = Date.parse(action.serverTime);
      if (Number.isNaN(parsed)) return state;
      return { offsetMs: parsed - action.receivedAt, lastSyncAt: action.receivedAt };
    }
    case 'clock.reset':
      return initialClockState;
    default:
      return state;
  }
}

/** Best estimate of the server's "now" for a given client epoch. */
export function serverNow(state: ClockState, clientNow: number): number {
  return clientNow + state.offsetMs;
}

export function isTimeUnverified(
  state: ClockState,
  clientNow: number,
  maxAgeMs: number = TIME_UNVERIFIED_AFTER_MS,
): boolean {
  if (state.lastSyncAt === null) return true;
  return clientNow - state.lastSyncAt > maxAgeMs;
}

/** True when the engine understands the given IANA zone. */
export function supportsTimeZone(timeZone: string): boolean {
  if (typeof Intl === 'undefined' || typeof Intl.DateTimeFormat !== 'function') return false;
  try {
    new Intl.DateTimeFormat('en-US', { timeZone }).format(0);
    return true;
  } catch {
    return false;
  }
}

export interface WallClockParts {
  year: number;
  month: number; // 1-12
  day: number; // 1-31
  hour: number; // 0-23
  minute: number;
  second: number;
  weekday: number; // 0 = Sunday
}

export interface ClockFormatOptions {
  timezone: string;
  utcOffsetSeconds: number;
  locale?: string | undefined;
  /** Override zone support (tests, and engines that lie). */
  timeZoneSupported?: boolean;
}

const WEEKDAY_NAMES = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday'];
const MONTH_NAMES = [
  'January', 'February', 'March', 'April', 'May', 'June',
  'July', 'August', 'September', 'October', 'November', 'December',
];

/**
 * Wall-clock components for `epochMs` in the home timezone. Uses named-zone
 * `Intl` when available, otherwise shifts the epoch by the fixed UTC offset.
 */
export function wallClockParts(epochMs: number, options: ClockFormatOptions): WallClockParts {
  const supported = options.timeZoneSupported ?? supportsTimeZone(options.timezone);
  if (supported) {
    const formatter = new Intl.DateTimeFormat('en-US', {
      timeZone: options.timezone,
      hourCycle: 'h23',
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
      weekday: 'short',
    });
    const parts = new Map<string, string>();
    for (const part of formatter.formatToParts(new Date(epochMs))) {
      parts.set(part.type, part.value);
    }
    const weekdayShort = parts.get('weekday') ?? 'Sun';
    return {
      year: Number(parts.get('year')),
      month: Number(parts.get('month')),
      day: Number(parts.get('day')),
      hour: Number(parts.get('hour')) % 24,
      minute: Number(parts.get('minute')),
      second: Number(parts.get('second')),
      weekday: WEEKDAY_NAMES.findIndex((name) => name.startsWith(weekdayShort)),
    };
  }

  const shifted = new Date(epochMs + options.utcOffsetSeconds * 1000);
  return {
    year: shifted.getUTCFullYear(),
    month: shifted.getUTCMonth() + 1,
    day: shifted.getUTCDate(),
    hour: shifted.getUTCHours(),
    minute: shifted.getUTCMinutes(),
    second: shifted.getUTCSeconds(),
    weekday: shifted.getUTCDay(),
  };
}

function pad2(value: number): string {
  return value < 10 ? `0${value}` : String(value);
}

export interface FormattedClock {
  /** 24-hour `HH:MM`, the very large element on the dashboard. */
  time: string;
  seconds: string;
  /** `Weekday, D Month YYYY` in the home timezone. */
  date: string;
  /** `YYYY-MM-DD` in the home timezone; changes exactly at local midnight. */
  dayKey: string;
}

export function formatClock(epochMs: number, options: ClockFormatOptions): FormattedClock {
  const parts = wallClockParts(epochMs, options);
  const weekday = WEEKDAY_NAMES[parts.weekday] ?? WEEKDAY_NAMES[0];
  const month = MONTH_NAMES[parts.month - 1] ?? '';
  return {
    time: `${pad2(parts.hour)}:${pad2(parts.minute)}`,
    seconds: pad2(parts.second),
    date: `${weekday}, ${parts.day} ${month} ${parts.year}`,
    dayKey: `${parts.year}-${pad2(parts.month)}-${pad2(parts.day)}`,
  };
}

/** Home-timezone day key, used for "captured today" labels and midnight rollover. */
export function homeDayKey(epochMs: number, options: ClockFormatOptions): string {
  return formatClock(epochMs, options).dayKey;
}

/** Short caption for a photo's captured date, e.g. `5 Sep 2026`. */
export function formatCapturedDate(
  isoTimestamp: string | null,
  options: ClockFormatOptions,
): string | null {
  if (!isoTimestamp) return null;
  const epoch = Date.parse(isoTimestamp);
  if (Number.isNaN(epoch)) return null;
  const parts = wallClockParts(epoch, options);
  const month = MONTH_NAMES[parts.month - 1]?.slice(0, 3) ?? '';
  return `${parts.day} ${month} ${parts.year} ${pad2(parts.hour)}:${pad2(parts.minute)}`;
}
