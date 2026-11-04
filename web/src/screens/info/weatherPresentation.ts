import { supportsTimeZone, wallClockParts } from '../../core/clock';
import type { ClockFormatOptions } from '../../core/clock';
import type { WeatherWidgetPayload } from '../../types/api';

const HOUR_MS = 3600000;
const WEEKDAYS = ['周日', '周一', '周二', '周三', '周四', '周五', '周六'];
const DIRECTIONS = ['北', '东北', '东', '东南', '南', '西南', '西', '西北'];

export type WeatherIconKind = 'sun' | 'partly' | 'cloud' | 'fog' | 'rain' | 'snow' | 'storm' | 'unknown';
const CONDITIONS: Record<string, { label: string; kind: WeatherIconKind }> = {
  clear: { label: '晴', kind: 'sun' }, mainly_clear: { label: '晴间多云', kind: 'partly' },
  partly_cloudy: { label: '多云', kind: 'partly' }, overcast: { label: '阴', kind: 'cloud' },
  fog: { label: '雾', kind: 'fog' }, drizzle: { label: '毛毛雨', kind: 'rain' },
  freezing_drizzle: { label: '冻毛毛雨', kind: 'rain' }, rain: { label: '雨', kind: 'rain' },
  freezing_rain: { label: '冻雨', kind: 'rain' }, snow: { label: '雪', kind: 'snow' },
  rain_showers: { label: '阵雨', kind: 'rain' }, snow_showers: { label: '阵雪', kind: 'snow' },
  thunderstorm: { label: '雷雨', kind: 'storm' }, thunderstorm_with_hail: { label: '雷雨伴冰雹', kind: 'storm' },
};
// The same closed WMO groups as the provider's ConditionText(), localized here
// so the wire format stays compatible with older clients and cached readings.
const WMO_CONDITIONS: Record<number, string> = {
  0: 'clear', 1: 'mainly_clear', 2: 'partly_cloudy', 3: 'overcast', 45: 'fog', 48: 'fog',
  51: 'drizzle', 53: 'drizzle', 55: 'drizzle', 56: 'freezing_drizzle', 57: 'freezing_drizzle',
  61: 'rain', 63: 'rain', 65: 'rain', 66: 'freezing_rain', 67: 'freezing_rain',
  71: 'snow', 73: 'snow', 75: 'snow', 77: 'snow', 80: 'rain_showers', 81: 'rain_showers', 82: 'rain_showers',
  85: 'snow_showers', 86: 'snow_showers', 95: 'thunderstorm', 96: 'thunderstorm_with_hail', 99: 'thunderstorm_with_hail',
};
const ENGLISH_ALIASES: Record<string, string> = { cloudy: 'partly_cloudy', sunny: 'clear', clear_sky: 'clear', showers: 'rain_showers' };
function conditionKey(value: string): string { const key = value.trim().toLowerCase().replace(/[\s-]+/g, '_'); return ENGLISH_ALIASES[key] ?? key; }

/** Preserve an explicit Chinese description. Otherwise known WMO codes or
 * legacy English labels produce Chinese; unrecognized input remains unknown. */
export function weatherConditionInfo(code: number | string | null | undefined, text: string | null | undefined): { label: string; kind: WeatherIconKind } {
  const numeric = typeof code === 'number' ? code : typeof code === 'string' && /^\d+$/.test(code.trim()) ? Number(code) : -1;
  const key = WMO_CONDITIONS[numeric];
  const condition = key ? CONDITIONS[key] : (text ? CONDITIONS[conditionKey(text)] : undefined)
    ?? (typeof code === 'string' ? CONDITIONS[conditionKey(code)] : undefined);
  const supplied = text?.trim() ?? '';
  return { label: /[\u3400-\u9fff]/.test(supplied) ? supplied : condition?.label ?? '天气待确认', kind: condition?.kind ?? 'unknown' };
}

function finite(value: unknown): value is number { return typeof value === 'number' && Number.isFinite(value); }
function temperature(value: unknown): string { return finite(value) ? `${Math.round(value)}°` : '—'; }
function clockTime(value: string | null | undefined, options: ClockFormatOptions | null): string | null {
  if (!value || !options || !Number.isFinite(Date.parse(value))) return null;
  const parts = wallClockParts(Date.parse(value), options);
  return `${String(parts.hour).padStart(2, '0')}:${String(parts.minute).padStart(2, '0')}`;
}
function dateKey(epoch: number, options: ClockFormatOptions): string {
  const parts = wallClockParts(epoch, options);
  return `${parts.year}-${String(parts.month).padStart(2, '0')}-${String(parts.day).padStart(2, '0')}`;
}
function validDate(date: string): boolean {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(date)) return false;
  const stamp = Date.parse(`${date}T00:00:00Z`);
  return Number.isFinite(stamp) && new Date(stamp).toISOString().slice(0, 10) === date;
}

export interface HourlyPoint {
  timestamp: number;
  temperature: number;
  time: string;
  x: number;
  y: number;
  current: boolean;
}
export interface ForecastDay {
  date: string;
  label: string;
  code: number | null;
  low: string;
  high: string;
  left: number;
  width: number;
  current: number | null;
}
export interface WeatherPresentation {
  temperature: string;
  range: string;
  updated: string | null;
  stats: Array<{ label: string; value: string; accent?: boolean }>;
  hourly: HourlyPoint[];
  line: string;
  area: string;
  days: ForecastDay[];
}

/** Shape-preserving cubic interpolation keeps the curve within neighboring
 * readings, rather than implying unobserved temperature peaks. */
function path(points: HourlyPoint[]): string {
  if (points.length < 2) return '';
  const slopes: number[] = [], tangents: number[] = [];
  for (let index = 0; index < points.length - 1; index += 1) {
    const a = points[index]!, b = points[index + 1]!;
    slopes.push((b.y - a.y) / (b.x - a.x));
  }
  tangents[0] = slopes[0]!; tangents[points.length - 1] = slopes[slopes.length - 1]!;
  for (let index = 1; index < points.length - 1; index += 1) {
    const a = slopes[index - 1]!, b = slopes[index]!;
    // Harmonic mean is monotone for nonuniform x intervals as well.
    tangents[index] = a * b <= 0 ? 0 : 2 * a * b / (a + b);
  }
  let value = `M${points[0]!.x.toFixed(1)},${points[0]!.y.toFixed(1)}`;
  for (let index = 0; index < points.length - 1; index += 1) {
    const a = points[index]!, b = points[index + 1]!, dx = (b.x - a.x) / 3;
    value += `C${(a.x + dx).toFixed(1)},${(a.y + tangents[index]! * dx).toFixed(1)} ${(b.x - dx).toFixed(1)},${(b.y - tangents[index + 1]! * dx).toFixed(1)} ${b.x.toFixed(1)},${b.y.toFixed(1)}`;
  }
  return value;
}

/** The forecast date belongs to the provider's reported timezone. Actual
 * instants (hourly/update/sunset) are formatted in the family's home timezone. */
export function presentWeather(weather: WeatherWidgetPayload | null, options: ClockFormatOptions | null, now: number): WeatherPresentation {
  const humidity = weather?.humidity_pct;
  const speed = weather?.wind_speed_kmh, direction = weather?.wind_direction_deg;
  const wind = finite(speed) && speed >= 0
    ? `${finite(direction) ? `${DIRECTIONS[Math.round((((direction % 360) + 360) % 360) / 45) % 8]} ` : ''}${Math.round(speed)} km/h` : '—';
  const aqi = weather?.air_quality?.aqi;
  const air = finite(aqi) && aqi >= 0 ? `${weather?.air_quality?.label?.trim() || 'AQI'} ${Math.round(aqi)}` : '暂无';
  const stats = [{ label: '空气', value: air, accent: finite(aqi) && aqi >= 0 },
    { label: '湿度', value: finite(humidity) && humidity >= 0 && humidity <= 100 ? `${Math.round(humidity)}%` : '—' },
    { label: '风', value: wind }, { label: '日落', value: clockTime(weather?.sunset, options) ?? '—' }];

  const observations: Array<{ timestamp: number; temperature: number; current: boolean }> = [];
  if (weather && !weather.stale && finite(weather.temperature_c)) observations.push({ timestamp: now, temperature: weather.temperature_c, current: true });
  const seen = new Set<number>();
  const candidates = (weather?.hourly ?? []).filter(item => finite(item.temperature_c) && Number.isFinite(Date.parse(item.time)))
    .map(item => ({ timestamp: Date.parse(item.time), temperature: item.temperature_c as number, current: false }))
    .sort((a, b) => a.timestamp - b.timestamp);
  let nextAt = now + (observations.length ? 3 * HOUR_MS : 0);
  for (const item of candidates) {
    if (seen.has(item.timestamp) || item.timestamp < nextAt) continue;
    seen.add(item.timestamp); observations.push(item); nextAt = item.timestamp + 3 * HOUR_MS;
    if (observations.length >= 9) break;
  }
  const hourly: HourlyPoint[] = [];
  if (options && observations.length >= 2) {
    const low = Math.min(...observations.map(point => point.temperature)) - 1;
    const high = Math.max(...observations.map(point => point.temperature)) + 1;
    const start = observations[0]!.timestamp, duration = observations[observations.length - 1]!.timestamp - start;
    for (const item of observations) {
      hourly.push({ ...item, time: item.current ? '现在' : `${String(wallClockParts(item.timestamp, options).hour).padStart(2, '0')}时`,
        x: 22 + (item.timestamp - start) / duration * 392, y: 30 + (1 - (item.temperature - low) / (high - low)) * 64 });
    }
  }
  const line = path(hourly);
  const area = line ? `${line}L${hourly[hourly.length - 1]!.x.toFixed(1)},104L${hourly[0]!.x.toFixed(1)},104Z` : '';

  const forecastOptions = weather?.forecast_timezone && supportsTimeZone(weather.forecast_timezone)
    ? { timezone: weather.forecast_timezone, utcOffsetSeconds: 0 } : options;
  const today = forecastOptions ? dateKey(now, forecastOptions) : null;
  const dates = new Set<string>();
  const daily = (weather?.daily ?? []).filter(day => validDate(day.date) && finite(day.min_c) && finite(day.max_c) && day.min_c <= day.max_c
    && today !== null && day.date >= today)
    .sort((a, b) => a.date.localeCompare(b.date)).filter(day => {
      if (dates.has(day.date)) return false;
      dates.add(day.date); return true;
    }).slice(0, 5);
  const allLows = daily.map(day => day.min_c as number), allHighs = daily.map(day => day.max_c as number);
  const minimum = daily.length ? Math.min(...allLows) : 0, maximum = daily.length ? Math.max(...allHighs) : 0;
  const lower = maximum === minimum ? minimum - 1 : minimum, upper = maximum === minimum ? maximum + 1 : maximum;
  const span = upper - lower || 1;
  const current = weather?.temperature_c;
  const days = daily.map(day => ({ date: day.date, label: day.date === today ? '今天' : WEEKDAYS[new Date(`${day.date}T00:00:00Z`).getUTCDay()] ?? day.date,
    code: finite(day.condition_code) ? day.condition_code : null, low: temperature(day.min_c), high: temperature(day.max_c),
    left: ((day.min_c as number) - lower) / span * 100, width: ((day.max_c as number) - (day.min_c as number)) / span * 100,
    current: day.date === today && !weather?.stale && finite(current) ? Math.max(0, Math.min(100, (current - lower) / span * 100)) : null }));
  return { temperature: temperature(weather?.temperature_c), range: `最高 ${temperature(weather?.temperature_max_c)} · 最低 ${temperature(weather?.temperature_min_c)}`,
    updated: clockTime(weather?.fetched_at, options), stats, hourly, line, area, days };
}
