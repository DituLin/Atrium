import { koiAlmanac, koiLunar, koiSolarTerms } from '../../core/koi';
import type { KoiSolarTerm } from '../../core/koi';
import { calendarFestivals } from '../../core/calendarFestivals';
// Text formatting only. Today never uses the older calendar's date arithmetic.
import { lunarDayName, lunarMonthName } from '../../core/lunar';
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
  previous: KoiSolarTerm;
  next: KoiSolarTerm;
  elapsed: number;
  total: number;
  remaining: number;
  percent: number;
}

export interface AlmanacInfo {
  lunar: string;
  /** KOI pillars change at the civil Lichun/Jie date, without a Spring Festival zodiac label. */
  year: string;
  month: string;
  day: string;
  yi: readonly string[];
  ji: readonly string[];
  today: string[];
  ruleVersion: string;
}

export interface AlmanacPresentation {
  info: AlmanacInfo | null;
  month: string;
  weekNumber: number;
  dayOfYear: number;
  week: AlmanacWeekDay[];
  term: TermProgress | null;
}

/** Calendar arithmetic uses UTC day numbers for the already-local family date.
 * It must never cross DST or reinterpret the date in the browser's own zone. */
export function presentAlmanac(day: Pick<FamilyDay, 'year' | 'month' | 'day'>): AlmanacPresentation {
  const koi = koiAlmanac(day);
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
    const lunar = koi ? koiLunar({ year, month, day: dateOfMonth }) : null;
    const special = lunar ? calendarFestivals(month, dateOfMonth, lunar)[0]
      ?? koiSolarTerms(year).find(term => term.month === month && term.day === dateOfMonth)?.name : undefined;
    week.push({ key: dateKey(year, month, dateOfMonth), day: dateOfMonth, weekday: WEEKDAYS[date.getUTCDay()] ?? '',
      lunar: special ?? (lunar ? lunar.day === 1 ? lunarMonthName(lunar) : lunarDayName(lunar.day) : '—'),
      today: monday + index === current, weekend: index >= 5, special: Boolean(special) });
  }
  const terms = [...koiSolarTerms(day.year - 1), ...koiSolarTerms(day.year), ...koiSolarTerms(day.year + 1)];
  let previous: KoiSolarTerm | undefined, next: KoiSolarTerm | undefined;
  for (const term of terms) {
    const number = dayNumber(term.year, term.month, term.day);
    if (number <= current) previous = term;
    else { next = term; break; }
  }
  let term: TermProgress | null = null;
  if (koi && previous && next) {
    const start = dayNumber(previous.year, previous.month, previous.day);
    const end = dayNumber(next.year, next.month, next.day);
    const total = end - start, elapsed = current - start;
    term = { previous, next, total, elapsed, remaining: end - current, percent: total > 0 ? elapsed / total * 100 : 0 };
  }
  let info: AlmanacInfo | null = null;
  if (koi) {
    const today = calendarFestivals(day.month, day.day, koi.lunar);
    const onTerm = terms.find(term => term.year === day.year && term.month === day.month && term.day === day.day);
    if (onTerm) today.push(onTerm.name);
    info = { lunar: `${lunarMonthName(koi.lunar)}${lunarDayName(koi.lunar.day)}`,
      year: `${koi.yearPillar}年`, month: `${koi.monthPillar}月`, day: `${koi.dayPillar}日`,
      yi: koi.yi, ji: koi.ji, today, ruleVersion: koi.ruleVersion };
  }
  return { info, month: `${day.year}年 · ${MONTHS[day.month - 1] ?? day.month}月`,
    weekNumber, dayOfYear: current - dayNumber(day.year, 1, 1) + 1, week, term };
}
