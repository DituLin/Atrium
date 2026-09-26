/**
 * Chinese lunisolar calendar (农历) for 1900-01-31 … 2101-01-28, computed
 * locally so the TV needs no network and no Intl calendar support (WebView 66
 * lacks `u-ca-chinese` parts). The month table was generated from ICU's Chinese
 * calendar and is verified day by day against it in lunar.test.ts.
 *
 * Each entry encodes one lunar year: bits 5–16 = months 1–12 (1 = 30 days),
 * bit 4 = the leap month has 30 days, bits 0–3 = leap month number (0 = none).
 */
const YEAR_TABLE: readonly number[] = [
  0x17a48, 0xea40, 0x1d4a0, 0x16545, 0xc960, 0x15360, 0x154d4, 0xad40, 0x16b20, 0x17542,
  0xea40, 0x1b4a6, 0x164a0, 0x14960, 0x14975, 0x55a0, 0xad60, 0xb633, 0x1b520, 0x1d257,
  0x1d240, 0x1a4a0, 0x1a1b6, 0x14ac0, 0x56c0, 0x15ab4, 0xda80, 0x1d520, 0x1e942, 0x1d240,
  0xd4c6, 0xa560, 0x14ae0, 0x12ad5, 0x16b40, 0xda80, 0xec33, 0xe920, 0x16277, 0x15260,
  0xa560, 0xa376, 0x155a0, 0xad40, 0x1b4b4, 0x17480, 0x16920, 0x1a962, 0x152a0, 0x155a7,
  0xa6c0, 0x155a0, 0x15955, 0x1b640, 0x1b480, 0x1d433, 0x1a940, 0xb2a8, 0x152e0, 0xaac0,
  0xaea6, 0x15aa0, 0xda40, 0xeaa4, 0x1d4a0, 0xc940, 0xc9e3, 0x15360, 0x15b47, 0xad40,
  0x16d20, 0x17645, 0x16a40, 0x164a0, 0x16564, 0x14960, 0x15568, 0x55a0, 0xada0, 0xb536,
  0x1b520, 0x1b240, 0x1d2a4, 0x1a4a0, 0x1c9aa, 0x14ac0, 0x56c0, 0x56b7, 0xdaa0, 0x1d520,
  0x1ea45, 0x1d240, 0x1a4c0, 0xa5c3, 0x14ae0, 0x15ac8, 0x6b40, 0xdaa0, 0xed25, 0xe920,
  0xd260, 0x15364, 0xa560, 0x14b60, 0x155c2, 0xad40, 0x1baa7, 0x17480, 0x16920, 0x1aa65,
  0x152a0, 0xa5a0, 0xa7a4, 0x156a0, 0x17549, 0xba40, 0x1b4a0, 0x1d156, 0x1c940, 0x192a0,
  0x153c4, 0xaac0, 0x156a0, 0x15b42, 0xda40, 0xeca6, 0x1e4a0, 0xc940, 0xcae5, 0x9560,
  0xab60, 0xadc3, 0x16d20, 0x1ea4b, 0x16a40, 0x164a0, 0x1a176, 0x14960, 0x9560, 0x5765,
  0xb5a0, 0x16d40, 0x1b542, 0x1b240, 0x1d4a7, 0x1a4a0, 0x14aa0, 0x149b5, 0x96c0, 0xb6a0,
  0xda53, 0x1d920, 0x1f248, 0x1d240, 0x1a4c0, 0xa2d6, 0x14ae0, 0x9ac0, 0x6cb4, 0xeaa0,
  0xe920, 0xe963, 0xd260, 0x15567, 0xa560, 0x14b60, 0x15745, 0xad40, 0x16ca0, 0x17544,
  0x16920, 0x1b2a8, 0x152a0, 0xa5a0, 0xada6, 0x156a0, 0xb540, 0xbaa4, 0x1b4a0, 0x1a940,
  0x1c9a3, 0x192c0, 0x199c7, 0xaac0, 0x156a0, 0x15a55, 0xda40, 0x1d4a0, 0xe544, 0xd160,
  0xd2e8, 0x9560, 0xab60, 0xaad6, 0x16d40, 0xea40, 0x172a4, 0x168a0, 0x15160, 0x149e2,
  0x9560,
];

const FIRST_YEAR = 1900;
const DAY_MS = 86400000;
/** Lunar 1900-01-01 = Gregorian 1900-01-31, as a UTC day number. */
const BASE_DAY = Date.UTC(1900, 0, 31) / DAY_MS;

const STEMS = '甲乙丙丁戊己庚辛壬癸';
const BRANCHES = '子丑寅卯辰巳午未申酉戌亥';
const ZODIAC = '鼠牛虎兔龙蛇马羊猴鸡狗猪';
const MONTH_NAMES = ['正', '二', '三', '四', '五', '六', '七', '八', '九', '十', '冬', '腊'];
const DIGITS = ['', '一', '二', '三', '四', '五', '六', '七', '八', '九', '十'];

export interface LunarDate {
  year: number;
  month: number;
  day: number;
  leap: boolean;
  /** Days in this lunar month (29 or 30). */
  monthLength: number;
}

function monthLength(entry: number, month: number): number {
  return (entry >> (4 + month)) & 1 ? 30 : 29;
}

function leapMonth(entry: number): number {
  return entry & 0xf;
}

function leapLength(entry: number): number {
  return (entry >> 4) & 1 ? 30 : 29;
}

function yearLength(entry: number): number {
  let days = 0;
  for (let month = 1; month <= 12; month++) days += monthLength(entry, month);
  return leapMonth(entry) ? days + leapLength(entry) : days;
}

function utcDay(year: number, month: number, day: number): number {
  const date = new Date(0);
  date.setUTCFullYear(year, month - 1, day);
  return Math.floor(date.getTime() / DAY_MS);
}

/** Converts a Gregorian calendar date (the family's local date) to 农历, or null outside 1900-01-31 … 2101-01-28. */
export function toLunar(year: number, month: number, day: number): LunarDate | null {
  let offset = utcDay(year, month, day) - BASE_DAY;
  if (!Number.isFinite(offset) || offset < 0) return null;
  for (const [index, entry] of YEAR_TABLE.entries()) {
    const length = yearLength(entry);
    if (offset >= length) { offset -= length; continue; }
    const leap = leapMonth(entry);
    for (let m = 1; m <= 12; m++) {
      const normal = monthLength(entry, m);
      if (offset < normal) return { year: FIRST_YEAR + index, month: m, day: offset + 1, leap: false, monthLength: normal };
      offset -= normal;
      if (m === leap) {
        const extra = leapLength(entry);
        if (offset < extra) return { year: FIRST_YEAR + index, month: m, day: offset + 1, leap: true, monthLength: extra };
        offset -= extra;
      }
    }
  }
  return null;
}

/** 正月 … 腊月, with 闰 for a leap month. */
export function lunarMonthName(date: LunarDate): string {
  return `${date.leap ? '闰' : ''}${MONTH_NAMES[date.month - 1] ?? ''}月`;
}

/** 初一 … 初十, 十一 … 二十, 廿一 … 廿九, 三十. */
export function lunarDayName(day: number): string {
  if (day <= 10) return `初${day === 10 ? '十' : DIGITS[day] ?? ''}`;
  if (day < 20) return `十${DIGITS[day - 10] ?? ''}`;
  if (day === 20) return '二十';
  if (day < 30) return `廿${DIGITS[day - 20] ?? ''}`;
  return '三十';
}

/** Stem-branch year (干支纪年) and zodiac, changing at 春节. */
export function ganzhiYear(lunarYear: number): { ganzhi: string; zodiac: string } {
  const cycle = (((lunarYear - 4) % 60) + 60) % 60;
  return { ganzhi: STEMS.charAt(cycle % 10) + BRANCHES.charAt(cycle % 12), zodiac: ZODIAC.charAt(cycle % 12) };
}

/** Stem-branch day (干支纪日); 1949-10-01 was 甲子日. */
export function ganzhiDay(year: number, month: number, day: number): string {
  const cycle = (((utcDay(year, month, day) - utcDay(1949, 10, 1)) % 60) + 60) % 60;
  return STEMS.charAt(cycle % 10) + BRANCHES.charAt(cycle % 12);
}

/* ------------------------------------------------------------ solar terms */

/** Ordered by solar longitude 0°, 15°, … 345°. */
const TERM_NAMES = [
  '春分', '清明', '谷雨', '立夏', '小满', '芒种', '夏至', '小暑', '大暑', '立秋', '处暑', '白露',
  '秋分', '寒露', '霜降', '立冬', '小雪', '大雪', '冬至', '小寒', '大寒', '立春', '雨水', '惊蛰',
];

const RAD = Math.PI / 180;

/** Apparent solar longitude in degrees (Meeus, ch. 25, low accuracy ≈ 0.01°). */
function solarLongitude(julianDay: number): number {
  const t = (julianDay - 2451545) / 36525;
  const l0 = 280.46646 + 36000.76983 * t + 0.0003032 * t * t;
  const m = (357.52911 + 35999.05029 * t - 0.0001537 * t * t) * RAD;
  const c = (1.914602 - 0.004817 * t - 0.000014 * t * t) * Math.sin(m)
    + (0.019993 - 0.000101 * t) * Math.sin(2 * m) + 0.000289 * Math.sin(3 * m);
  const omega = (125.04 - 1934.136 * t) * RAD;
  const apparent = l0 + c - 0.00569 - 0.00478 * Math.sin(omega);
  return ((apparent % 360) + 360) % 360;
}

function termJulianDay(year: number, longitude: number): number {
  // Start near the expected date: 0° falls around March 20.
  let jd = 2451545 + (year - 2000) * 365.2422 + 79.5 + (longitude / 360) * 365.2422;
  if (longitude >= 285) jd -= 365.2422; // 小寒 … 惊蛰 of `year` precede that year's 春分.
  for (let i = 0; i < 50; i++) {
    let delta = longitude - solarLongitude(jd);
    delta = ((delta + 540) % 360) - 180;
    jd += (delta / 360) * 365.2422;
    if (Math.abs(delta) < 1e-7) break;
  }
  return jd;
}

export interface SolarTerm {
  name: string;
  /**
   * Gregorian date of the term in China Standard Time (UTC+8), as the almanac
   * prints it. The low-precision solar model is accurate to about 15 minutes,
   * so a term within minutes of Beijing midnight can land one day off.
   */
  year: number;
  month: number;
  day: number;
}

const termCache = new Map<number, SolarTerm[]>();

/** The 24 solar terms falling in Gregorian `year`, in date order. */
export function solarTerms(year: number): SolarTerm[] {
  const cached = termCache.get(year);
  if (cached) return cached;
  const terms = TERM_NAMES.map((name, index) => {
    const utcMs = (termJulianDay(year, index * 15) - 2440587.5) * DAY_MS;
    const beijing = new Date(utcMs + 8 * 3600000);
    return { name, year: beijing.getUTCFullYear(), month: beijing.getUTCMonth() + 1, day: beijing.getUTCDate() };
  }).sort((a, b) => utcDay(a.year, a.month, a.day) - utcDay(b.year, b.month, b.day));
  termCache.set(year, terms);
  return terms;
}

/* ------------------------------------------------------------ almanac */

const LUNAR_FESTIVALS: Record<string, string> = {
  '1-1': '春节', '1-15': '元宵节', '2-2': '龙抬头', '5-5': '端午节', '7-7': '七夕',
  '7-15': '中元节', '8-15': '中秋节', '9-9': '重阳节', '12-8': '腊八节', '12-23': '小年',
};
const SOLAR_FESTIVALS: Record<string, string> = {
  '1-1': '元旦', '5-1': '劳动节', '10-1': '国庆节',
};

export interface Almanac {
  /** 八月十六 */
  lunar: string;
  /** 丙午马年 */
  year: string;
  /** 甲子日 */
  day: string;
  /** Festivals and the solar term that fall on this date, most important first. */
  today: string[];
  /** The next solar term strictly after this date. */
  nextTerm: { name: string; inDays: number } | null;
}

/** Everything the 今日 page prints for one Gregorian family date. */
export function almanac(year: number, month: number, day: number): Almanac | null {
  const lunar = toLunar(year, month, day);
  if (!lunar) return null;
  const { ganzhi, zodiac } = ganzhiYear(lunar.year);
  const today: string[] = [];
  if (!lunar.leap) {
    const festival = LUNAR_FESTIVALS[`${lunar.month}-${lunar.day}`];
    if (festival) today.push(festival);
    if (lunar.month === 12 && lunar.day === lunar.monthLength) today.push('除夕');
  }
  const solar = SOLAR_FESTIVALS[`${month}-${day}`];
  if (solar) today.push(solar);
  const dayNumber = utcDay(year, month, day);
  const terms = [...solarTerms(year), ...solarTerms(year + 1)];
  const term = terms.find(t => utcDay(t.year, t.month, t.day) === dayNumber);
  if (term) today.push(term.name);
  const next = terms.find(t => utcDay(t.year, t.month, t.day) > dayNumber);
  return {
    lunar: `${lunarMonthName(lunar)}${lunarDayName(lunar.day)}`,
    year: `${ganzhi}${zodiac}年`,
    day: `${ganzhiDay(year, month, day)}日`,
    today,
    nextTerm: next ? { name: next.name, inDays: utcDay(next.year, next.month, next.day) - dayNumber } : null,
  };
}
