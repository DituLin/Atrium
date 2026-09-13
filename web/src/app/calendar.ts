import { supportsTimeZone, wallClockParts } from '../core/clock';
export interface CalendarMonth { year: number; month: number }
export function calendarDate(epoch: number, timezone: string | undefined) {
 if (!timezone || !supportsTimeZone(timezone) || !Number.isFinite(epoch)) return null;
 return wallClockParts(epoch, { timezone, utcOffsetSeconds: 0 });
}
export function shiftMonth(value: CalendarMonth, delta: number): CalendarMonth {
 const index = value.year * 12 + value.month - 1 + delta;
 return { year: Math.floor(index / 12), month: ((index % 12) + 12) % 12 + 1 };
}
export function monthCells(year: number, month: number): (number | null)[] {
 // setUTCFullYear avoids Date.UTC's special treatment of years 0–99.
 const date = new Date(0); date.setUTCFullYear(year, month - 1, 1);
 const start = (date.getUTCDay() + 6) % 7;
 date.setUTCFullYear(year, month, 0);
 const length = date.getUTCDate();
 return Array.from({ length: 42 }, (_, index) => index >= start && index < start + length ? index - start + 1 : null);
}
