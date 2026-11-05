import lunarFile from './data/lunar_table.json';
import termsFile from './data/solar_terms.json';
import rulesFile from './data/rules.json';
import taishenFile from './data/taishen_60.json';
import pengzuFile from './data/pengzu.json';
import xiuAnchors from './data/anchor_28xiu.json';

/** A civil date already selected in the family's time zone. No browser zone is consulted. */
export interface CivilDate { year: number; month: number; day: number }

export interface KoiLunarDate extends CivilDate { leap: boolean; monthLength: number }
export interface KoiSolarTerm extends CivilDate { name: string; at: string }
export interface KoiHour { zhi: string; officer: string; lucky: boolean }
export interface KoiAlmanacDay {
  lunar: KoiLunarDate;
  yearPillar: string;
  monthPillar: string;
  dayPillar: string;
  zodiac: string;
  nayin: string;
  jianchu: string;
  zhishen: string;
  zhishenLucky: boolean;
  chong: string;
  sha: string;
  auspiciousDeities: readonly string[];
  inauspiciousDeities: readonly string[];
  yi: readonly string[];
  ji: readonly string[];
  hours: readonly KoiHour[];
  xiu: string | null;
  taishen: string | null;
  pengzu: readonly string[];
  ruleVersion: string;
}

export const KOI_SOURCE = {
  name: '锦鲤万年历',
  repository: 'https://github.com/DituLin/Koi',
  commit: 'f973da1b07a8bc3a387c202fd1a134f24924a3fc',
  ruleVersion: 'koi-almanac/1.0',
  calendarVersion: '1.0.0',
  firstDate: '1901-01-01',
  lastDate: '2099-12-31',
} as const;

/**
 * Port of Koi's pure calendar/almanac engines at the revision above. The JSON
 * bytes, upstream paths, generator provenance and SHA-256s are retained in
 * provenance.json. Calendar tables were generated upstream with sxtwl; the
 * almanac is Koi's versioned simplified traditional rule set, not a published
 * official daily almanac. There is no run-time network or Koi service dependency.
 *
 * Inputs are already-local civil dates. Koi's calendar/term convention is UTC+8;
 * all arithmetic below is on UTC day numbers to avoid browser time zones/DST.
 */
const DAY_MS = 86400000;
const FIRST_DAY = Date.UTC(1901, 0, 1) / DAY_MS;
const LAST_DAY = Date.UTC(2099, 11, 31) / DAY_MS;
const DAY_ANCHOR = Date.UTC(1949, 9, 1) / DAY_MS;
const JIE_NAMES = ['立春', '惊蛰', '清明', '立夏', '芒种', '小暑', '立秋', '白露', '寒露', '立冬', '大雪', '小寒'];
const XIU_NAMES = ['角', '亢', '氐', '房', '心', '尾', '箕', '斗', '牛', '女', '虚', '危', '室', '壁',
  '奎', '娄', '胃', '昴', '毕', '觜', '参', '井', '鬼', '柳', '星', '张', '翼', '轸'];

interface LunarYearRecord { ny: string; leap: number; months: number[]; firstMonth?: number }
interface MonthRecord { start: number; days: number; year: number; month: number; leap: boolean }
interface TermRecord extends KoiSolarTerm { epochDay: number }
interface JieRecord { epochDay: number; index: number }
interface SanheGroup { members: number[]; shaFang: string; jieSha: number; zaiSha: number }
interface RuleTables {
  ruleVersion: string;
  gan: string[]; zhi: string[]; zodiac: string[];
  jianchu: string[]; zhishen: string[]; zhishenHuangdao: string[];
  qinglongStart: Record<string, number>; nayin: string[]; sanhe: SanheGroup[];
  liuhe: Record<number, number>; liuhai: Record<number, number>; yuede: Record<number, number>;
  tiande: Record<number, { kind: string; index: number }>; heGan: Record<number, number>;
  sifei: Record<string, number[][]>; mucang: Record<string, number[]>;
  jianchuYiji: Record<string, { yi: string[]; ji: string[] }>;
  shenYi: Record<string, string[]>; shenJi: Record<string, string[]>;
}

const rules: RuleTables = rulesFile;
const lunarYears: Record<string, LunarYearRecord> = lunarFile.years;
const termYears: Record<string, { name: string; at: string }[]> = termsFile.years;
const taishen: Record<string, string> = taishenFile;
const pengzu: { gan: Record<string, string>; zhi: Record<string, string> } = pengzuFile;

function mod(value: number, divisor: number): number { return ((value % divisor) + divisor) % divisor; }

function civilFromIso(iso: string): CivilDate {
  return { year: Number(iso.slice(0, 4)), month: Number(iso.slice(5, 7)), day: Number(iso.slice(8, 10)) };
}

function epochDay(date: CivilDate): number { return Date.UTC(date.year, date.month - 1, date.day) / DAY_MS; }

function supportedDay(date: CivilDate): number | null {
  if (!Number.isInteger(date.year) || !Number.isInteger(date.month) || !Number.isInteger(date.day)
    || date.year < 1901 || date.year > 2099 || date.month < 1 || date.month > 12 || date.day < 1 || date.day > 31) return null;
  const value = epochDay(date);
  const checked = new Date(value * DAY_MS);
  return checked.getUTCFullYear() === date.year && checked.getUTCMonth() + 1 === date.month && checked.getUTCDate() === date.day ? value : null;
}

function civilFromDay(day: number): CivilDate {
  const date = new Date(day * DAY_MS);
  return { year: date.getUTCFullYear(), month: date.getUTCMonth() + 1, day: date.getUTCDate() };
}

function lunarKey(year: number, month: number, leap: boolean): string { return `${year}-${month}-${leap ? 1 : 0}`; }

const months: MonthRecord[] = [];
const monthIndex = new Map<string, MonthRecord>();
for (const yearText of Object.keys(lunarYears).sort()) {
  const record = lunarYears[yearText]!;
  let start = epochDay(civilFromIso(record.ny));
  let month = record.firstMonth ?? 1;
  let leapDone = false;
  for (const days of record.months) {
    const leap = record.leap !== 0 && month === record.leap + 1 && !leapDone;
    const lunarMonth = leap ? record.leap : month++;
    if (leap) leapDone = true;
    const value = { start, days, year: Number(yearText), month: lunarMonth, leap };
    months.push(value);
    monthIndex.set(lunarKey(value.year, lunarMonth, leap), value);
    start += days;
  }
}

const termsByYear = new Map<number, readonly TermRecord[]>();
const jieDays: JieRecord[] = [];
for (const [year, terms] of Object.entries(termYears)) {
  const records = terms.map(term => {
    const date = civilFromIso(term.at);
    const value = { ...date, ...term, epochDay: epochDay(date) };
    const index = JIE_NAMES.indexOf(term.name);
    if (index !== -1) jieDays.push({ epochDay: value.epochDay, index });
    return value;
  }).sort((a, b) => a.epochDay - b.epochDay);
  termsByYear.set(Number(year), records);
}
jieDays.sort((a, b) => a.epochDay - b.epochDay);

const xiuAnchor = xiuAnchors[0]!;
const xiuOffset = XIU_NAMES.indexOf(xiuAnchor.xiu) - epochDay(civilFromIso(xiuAnchor.date));

/** Last array index whose ordered day is <= target, or -1 before the first entry. */
function lastAtOrBefore<T>(items: readonly T[], target: number, day: (item: T) => number): number {
  let low = 0, high = items.length;
  while (low < high) {
    const middle = Math.floor((low + high) / 2);
    if (day(items[middle]!) <= target) low = middle + 1;
    else high = middle;
  }
  return low - 1;
}

/** Returns null for invalid civil dates or dates outside 1901-01-01…2099-12-31. */
export function koiLunar(date: CivilDate): KoiLunarDate | null {
  const day = supportedDay(date);
  if (day === null) return null;
  const record = months[lastAtOrBefore(months, day, item => item.start)];
  if (!record || day - record.start >= record.days) return null;
  return { year: record.year, month: record.month, day: day - record.start + 1, leap: record.leap, monthLength: record.days };
}

/** Rejects nonexistent leap months, short-month day 30, and results outside the civil range. */
export function koiFromLunar(lunar: CivilDate & { leap: boolean }): CivilDate | null {
  if (!Number.isInteger(lunar.year) || !Number.isInteger(lunar.month) || !Number.isInteger(lunar.day)
    || typeof lunar.leap !== 'boolean') return null;
  const record = monthIndex.get(lunarKey(lunar.year, lunar.month, lunar.leap));
  if (!record || lunar.day < 1 || lunar.day > record.days) return null;
  const day = record.start + lunar.day - 1;
  return day >= FIRST_DAY && day <= LAST_DAY ? civilFromDay(day) : null;
}

/** Bundled exact UTC+8 civil dates and minute instants. Returned values cannot mutate the tables. */
export function koiSolarTerms(year: number): readonly KoiSolarTerm[] {
  return (termsByYear.get(year) ?? []).map(({ name, year: termYear, month, day, at }) => ({ name, year: termYear, month, day, at }));
}

/**
 * Daily Koi almanac. Pillars/zodiac change for the entire 立春/节 civil day,
 * independently of the lunar year's 正月初一 boundary. All returned values are
 * fresh records/arrays; consumers cannot change the shared source tables.
 */
export function koiAlmanac(date: CivilDate): KoiAlmanacDay | null {
  const day = supportedDay(date);
  if (day === null) return null;
  const lunar = koiLunar(date);
  const lichun = termsByYear.get(date.year)?.find(term => term.name === '立春');
  if (!lunar || !lichun) return null;
  const yearIndex = mod((day >= lichun.epochDay ? date.year : date.year - 1) - 1984, 60);
  const jiePosition = lastAtOrBefore(jieDays, day, item => item.epochDay);
  // The upstream range includes 1901-01-01…01-05 but its term file begins in
  // 1901. Those days necessarily remain in the preceding 大雪 month (index 10)
  // until bundled 小寒. This fixes the source engine's missing predecessor;
  // it does NOT invent a date or instant for an unbundled 1900 solar term.
  const jieIndex = jiePosition >= 0 ? jieDays[jiePosition]!.index : 10;
  const monthZhi = (jieIndex + 2) % 12;
  const monthGan = ((yearIndex % 10 % 5) * 2 + 2 + jieIndex) % 10;
  const dayIndex = mod(day - DAY_ANCHOR, 60);
  const dayGan = dayIndex % 10, dayZhi = dayIndex % 12;
  const pillar = (gan: number, zhi: number): string => rules.gan[gan]! + rules.zhi[zhi]!;
  const dayPillar = pillar(dayGan, dayZhi);
  const jianchu = rules.jianchu[mod(dayZhi - monthZhi, 12)]!;
  const zhishen = rules.zhishen[mod(dayZhi - rules.qinglongStart[rules.zhi[monthZhi]!]!, 12)]!;
  const monthGroup = rules.sanhe.find(group => group.members.includes(monthZhi))!;
  const dayGroup = rules.sanhe.find(group => group.members.includes(dayZhi))!;
  const season = '春夏秋冬'.charAt(Math.floor(jieIndex / 3));

  const shen: string[] = [];
  const yuede = rules.yuede[monthZhi]!;
  if (yuede === dayGan) shen.push('月德');
  if (rules.heGan[yuede] === dayGan) shen.push('月德合');
  const tiande = rules.tiande[monthZhi]!;
  if ((tiande.kind === 'gan' && tiande.index === dayGan) || (tiande.kind === 'zhi' && tiande.index === dayZhi)) shen.push('天德');
  if (tiande.kind === 'gan' && rules.heGan[tiande.index] === dayGan) shen.push('天德合');
  if (rules.liuhe[monthZhi] === dayZhi) shen.push('六合');
  if (dayZhi !== monthZhi && monthGroup.members.includes(dayZhi)) shen.push('三合');
  if (rules.mucang[season]!.includes(dayZhi)) shen.push('母仓');

  const sha: string[] = [];
  if (mod(dayZhi - monthZhi, 12) === 6) sha.push('月破');
  if (dayZhi === monthGroup.jieSha) sha.push('劫煞');
  if (dayZhi === monthGroup.zaiSha) sha.push('灾煞');
  if (rules.liuhai[monthZhi] === dayZhi) sha.push('月害');
  if (rules.sifei[season]!.some(([gan, zhi]) => gan === dayGan && zhi === dayZhi)) sha.push('四废');

  const baseline = rules.jianchuYiji[jianchu]!;
  const yi = new Set(baseline.yi), ji = new Set(baseline.ji);
  for (const deity of shen) for (const activity of rules.shenYi[deity] ?? []) yi.add(activity);
  for (const deity of sha) for (const activity of rules.shenJi[deity] ?? []) ji.add(activity);
  if (ji.has('诸事不宜')) {
    for (const activity of yi) if (!['祭祀', '拆卸', '治病', '扫舍'].includes(activity)) yi.delete(activity);
  }
  for (const activity of ji) if (activity !== '诸事不宜') yi.delete(activity);
  const hourStart = rules.qinglongStart[rules.zhi[dayZhi]!]!;
  const hours = rules.zhi.map((zhi, index) => {
    const officer = rules.zhishen[mod(index - hourStart, 12)]!;
    return { zhi, officer, lucky: rules.zhishenHuangdao.includes(officer) };
  });

  return {
    lunar,
    yearPillar: pillar(yearIndex % 10, yearIndex % 12),
    monthPillar: pillar(monthGan, monthZhi),
    dayPillar, zodiac: rules.zodiac[yearIndex % 12]!,
    nayin: rules.nayin[Math.floor(dayIndex / 2)]!, jianchu, zhishen,
    zhishenLucky: rules.zhishenHuangdao.includes(zhishen),
    chong: rules.zodiac[(dayZhi + 6) % 12]!, sha: dayGroup.shaFang,
    auspiciousDeities: shen, inauspiciousDeities: sha,
    // Default JS sort is Unicode order, matching Python/Kotlin/Swift fixtures;
    // localeCompare would change stable upstream ordering between devices.
    yi: [...yi].sort(), ji: [...ji].sort(), hours,
    xiu: XIU_NAMES[mod(day + xiuOffset, 28)]!, taishen: taishen[dayPillar] ?? null,
    pengzu: [pengzu.gan[rules.gan[dayGan]!]!, pengzu.zhi[rules.zhi[dayZhi]!]!],
    ruleVersion: rules.ruleVersion,
  };
}
