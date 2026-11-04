import type { HouseSample, HouseSeries } from './model';
export interface HouseChartPath { line: string; area: string }
export interface HouseChart { paths: HouseChartPath[]; reference: HouseChartPath[]; last: { x: number; y: number } | null; min: number | null; max: number | null; low: number; high: number }
const DAY = 24 * 3600000;

/** Timestamp placement and explicit gaps are preserved; no synthetic readings are inserted. */
export function buildHouseChart(series: HouseSeries | null, width = 520, height = 190, fixedRange?: [number, number]): HouseChart {
  const end = Date.parse(series?.endAt ?? '');
  const normalize = (samples: HouseSample[]) => {
    const unique = new Map<number, number | null>();
    for (const sample of samples) {
      const at = Date.parse(sample.at);
      if (!Number.isFinite(at) || !Number.isFinite(end) || at < end - DAY || at > end) continue;
      unique.set(at, sample.value !== null && Number.isFinite(sample.value) ? sample.value : null);
    }
    return Array.from(unique, ([at, value]) => ({ at, value })).sort((a, b) => a.at - b.at);
  };
  const samples = normalize(series?.samples ?? []), reference = normalize(series?.reference?.samples ?? []);
  const values: number[] = [];
  for (const sample of samples) if (sample.value !== null) values.push(sample.value);
  const allValues = values.slice();
  for (const sample of reference) if (sample.value !== null) allValues.push(sample.value);
  const min = values.length ? Math.min(...values) : null, max = values.length ? Math.max(...values) : null;
  const bottom = allValues.length ? Math.min(...allValues) : 0, top = allValues.length ? Math.max(...allValues) : 1;
  const padding = Math.max((top - bottom) * .18, .5);
  const [low, high] = fixedRange ?? [bottom - padding, top + padding];
  const x = (at: number) => (at - (end - DAY)) / DAY * width;
  const y = (value: number) => 6 + (1 - (value - low) / (high - low || 1)) * (height - 12);
  const paths = (items: typeof samples): HouseChartPath[] => {
    const segments: Array<Array<{ at: number; value: number }>> = [];
    let segment: Array<{ at: number; value: number }> = [];
    const flush = () => { if (segment.length) segments.push(segment); segment = []; };
    for (const sample of items) {
      if (sample.value === null) { flush(); continue; }
      if (segment.length && sample.at - segment[segment.length - 1]!.at > (series?.maxGapMs ?? 2 * 3600000)) flush();
      segment.push({ at: sample.at, value: sample.value });
    }
    flush();
    return segments.map(points => {
      const first = points[0]!, last = points[points.length - 1]!;
      const line = points.map((point, index) => `${index ? 'L' : 'M'}${x(point.at).toFixed(1)},${y(point.value).toFixed(1)}`).join(' ');
      return { line, area: `${line}L${x(last.at).toFixed(1)},${height}L${x(first.at).toFixed(1)},${height}Z` };
    });
  };
  const last = [...samples].reverse().find(sample => sample.value !== null);
  return { paths: paths(samples), reference: paths(reference), last: last ? { x: x(last.at), y: y(last.value!) } : null, min, max, low, high };
}
