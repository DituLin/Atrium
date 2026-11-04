import { almanac, lunarDayName, lunarMonthName, solarTerms, toLunar } from '../../core/lunar';
import type { Almanac, SolarTerm } from '../../core/lunar';
import type { FamilyDay } from './familyDate';

const DAY_MS = 86400000;
const WEEKDAYS = ['日', '一', '二', '三', '四', '五', '六'];
const MONTHS = ['一', '二', '三', '四', '五', '六', '七', '八', '九', '十', '十一', '十二'];

function dayNumber(year: number, month: number, day: number): number {
  return Date.UTC(year, month - 1, day) / DAY_MS;
}

function dateKey(year: number, month: number, day: number): string {
  return `${year}-${String(month).padStart(2, '0')}-${String(day).padStart(2, '0')}`;
}

export interface AlmanacWeekDay {
  key: string;
  day: number;
  weekday: string;
  lunar: string;
  today: boolean;
  weekend: boolean;
  special: boolean;
}

export interface TermProgress {
  previous: SolarTerm;
  next: SolarTerm;
  elapsed: number;
  total: number;
  remaining: number;
  percent: number;
}

export interface AlmanacPresentation {
  info: Almanac | null;
  month: string;
  weekNumber: number;
  dayOfYear: number;
  week: AlmanacWeekDay[];
  term: TermProgress | null;
}

/** Calendar arithmetic uses UTC day numbers for the already-local family date.
 * It must never cross DST or reinterpret the date in the browser's own zone. */
export function presentAlmanac(day: Pick<FamilyDay, 'year' | 'month' | 'day'>): AlmanacPresentation {
  const current = dayNumber(day.year, day.month, day.day);
  const weekday = new Date(current * DAY_MS).getUTCDay();
  const monday = current - (weekday + 6) % 7;
  const thursday = new Date((monday + 3) * DAY_MS);
  const weekYear = thursday.getUTCFullYear();
  const weekNumber = Math.ceil((monday + 4 - dayNumber(weekYear, 1, 1)) / 7);
  const week: AlmanacWeekDay[] = [];
  for (let index = 0; index < 7; index += 1) {
    const date = new Date((monday + index) * DAY_MS);
    const year = date.getUTCFullYear(), month = date.getUTCMonth() + 1, dateOfMonth = date.getUTCDate();
    const lunar = toLunar(year, month, dateOfMonth);
    const special = almanac(year, month, dateOfMonth)?.today[0];
    week.push({ key: dateKey(year, month, dateOfMonth), day: dateOfMonth, weekday: WEEKDAYS[date.getUTCDay()] ?? '',
      lunar: special ?? (lunar ? lunar.day === 1 ? lunarMonthName(lunar) : lunarDayName(lunar.day) : '—'),
      today: monday + index === current, weekend: index >= 5, special: Boolean(special) });
  }
  const terms = [...solarTerms(day.year - 1), ...solarTerms(day.year), ...solarTerms(day.year + 1)];
  let previous: SolarTerm | undefined, next: SolarTerm | undefined;
  for (const term of terms) {
    const number = dayNumber(term.year, term.month, term.day);
    if (number <= current) previous = term;
    else { next = term; break; }
  }
  let term: TermProgress | null = null;
  if (previous && next) {
    const start = dayNumber(previous.year, previous.month, previous.day);
    const end = dayNumber(next.year, next.month, next.day);
    const total = end - start, elapsed = current - start;
    term = { previous, next, total, elapsed, remaining: end - current, percent: total > 0 ? elapsed / total * 100 : 0 };
  }
  return { info: almanac(day.year, day.month, day.day), month: `${day.year}年 · ${MONTHS[day.month - 1] ?? day.month}月`,
    weekNumber, dayOfYear: current - dayNumber(day.year, 1, 1) + 1, week, term };
}
