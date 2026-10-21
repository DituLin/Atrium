import { memo, useId } from 'react';
import { chartDay, chartTime } from './TrendPlot';
import { trendSegments } from './trendCurve';
import type { WhoReference } from './whoGrowth';
import { ageMonthsAt } from './whoGrowth';

export interface GrowthCurvePlotProps {
    points: readonly { date: string; value: string }[];
    label: string;
    unit: string;
    timezone: string;
    reference?: WhoReference | null;
    birthDate?: string;
}

const tickValue = (value: number) => Math.abs(value) >= 1e7 ? value.toExponential(1) : Number(value.toPrecision(8)).toLocaleString('zh-CN', { maximumFractionDigits: 8 });

/** Measurements use the household calendar; the graph never invents missing days. */
export const GrowthCurvePlot = memo(function GrowthCurvePlot({ points, label, unit, timezone, reference, birthDate }: GrowthCurvePlotProps) {
    const gradient = `growth-curve-${useId().replace(/:/g, '')}`;
    const sorted = points.filter(point => Number.isFinite(chartTime(point.date)) && point.value.trim() !== '' && Number.isFinite(Number(point.value)) && Math.abs(Number(point.value)) <= Number.MAX_SAFE_INTEGER)
        .map(point => ({ ...point, time: chartDay(point.date, timezone), numeric: Number(point.value) }))
        .sort((a, b) => a.time - b.time || chartTime(a.date) - chartTime(b.date));
    const last = sorted[sorted.length - 1];
    const measuredAges = birthDate ? sorted.map(point => ageMonthsAt(point.date, birthDate, timezone)) : [];
    const referencePoints = reference?.points.filter(point => Number.isFinite(point.ageMonths) && Number.isFinite(point.p3) && Number.isFinite(point.p50) && Number.isFinite(point.p97) && Number.isFinite(chartTime(point.date))) ?? [];
    const ageAxis = !!birthDate && referencePoints.length > 0 && measuredAges.length === sorted.length && measuredAges.every(age => age !== null && Number.isFinite(age));
    const standards = ageAxis ? referencePoints : [];
    const values = [...sorted.map(point => point.numeric), ...standards.reduce<number[]>((result, point) => { result.push(point.p3, point.p97); return result; }, [])];
    if (!values.length) return null;
    const low = Math.min(...values), high = Math.max(...values);
    const padding = Math.max((high - low) * .15, Math.abs(high) * .025, .1);
    const rawStep = (high - low + padding * 2) / 6;
    const power = 10 ** Math.floor(Math.log10(rawStep));
    const step = [1, 2, 2.5, 5, 10].find(value => value * power >= rawStep)! * power;
    const min = Math.floor((low - padding) / step) * step;
    const max = Math.ceil((high + padding) / step) * step;
    const yTicks = Array.from({ length: Math.round((max - min) / step) + 1 }, (_, index) => min + step * index);
    const positions = ageAxis ? measuredAges as number[] : sorted.map(point => point.time);
    const coordinates = [...positions, ...standards.map(point => point.ageMonths)];
    let start = Math.min(...coordinates);
    let end = Math.max(...coordinates);
    let ageInterval: number | null = null;
    if (ageAxis) {
        const target = (end - start) / 4;
        ageInterval = [0.25, .5, 1, 2, 3, 6, 12, 24, 60].find(value => value >= target) ?? 60;
        start = Math.max(0, Math.floor(start / ageInterval) * ageInterval);
        end = Math.max(start + ageInterval, Math.ceil(end / ageInterval) * ageInterval);
    }
    const x = (position: number) => start === end ? 420 : (position - start) / (end - start) * 840;
    const y = (value: number) => 358 - (value - min) / (max - min) * 344;
    const curves = trendSegments(sorted.map((point, index) => ({ x: x(positions[index]!), y: y(point.numeric) })), 358);
    const dateLabel = (time: number) => `${new Date(time).getUTCMonth() + 1}月${new Date(time).getUTCDate()}日`;
    let ticks = start === end ? [start] : Array.from({ length: 4 }, (_, index) => start + (end - start) * index / 3);
    if (ageInterval !== null) {
        ticks = Array.from({ length: Math.round((end - start) / ageInterval) + 1 }, (_, index) => start + index * ageInterval!);
    }
    const referenceRuns: typeof standards[] = [];
    for (const point of standards) {
        const run = referenceRuns[referenceRuns.length - 1];
        if (!run || run[run.length - 1]!.basis !== point.basis) referenceRuns.push([point]);
        else run.push(point);
    }
    const medianDifference = reference?.medianDifference === null || reference?.medianDifference === undefined ? null : Number(reference.medianDifference.toFixed(2));
    const comparisonLabel = medianDifference === null ? null : `${medianDifference > 0 ? '+' : ''}${medianDifference} ${unit}`;
    return <svg className="growth-curve__plot" viewBox="0 0 990 426" role="img" aria-label={label}>
        <defs><linearGradient id={gradient} x1="0" y1="0" x2="0" y2="1"><stop offset="0%" stopColor="currentColor" stopOpacity=".4"/><stop offset="100%" stopColor="currentColor" stopOpacity="0"/></linearGradient></defs>
        <g data-axis="y">{yTicks.map((tick, index) => <g key={index}><line x1="0" x2="840" y1={y(tick)} y2={y(tick)} className="growth-curve__gridline"/><text x="918" y={y(tick) + 7}>{tickValue(tick)}</text></g>)}</g>
        {referenceRuns.map((run, index) => {
            const band = run.length > 1 ? `M ${run.map(point => `${x(point.ageMonths)} ${y(point.p97)}`).join(' L ')} L ${[...run].reverse().map(point => `${x(point.ageMonths)} ${y(point.p3)}`).join(' L ')} Z` : '';
            const median = trendSegments(run.map(point => ({ x: x(point.ageMonths), y: y(point.p50) })), 358);
            return <g key={index}>{band ? <path data-who-band data-basis={run[0]!.basis} d={band} className="growth-curve__who-band"/> : null}{(['p3', 'p97'] as const).map(boundary => <path key={boundary} d={`M ${run.map(point => `${x(point.ageMonths)} ${y(point[boundary])}`).join(' L ')}`} className="growth-curve__who-boundary"/>)}{median.map((curve, medianIndex) => <path data-who-median data-basis={run[0]!.basis} key={medianIndex} d={curve.line} className="growth-curve__who-median"/>)}</g>;
        })}
        {curves.map((curve, index) => <g key={index}>{curve.fill ? <path data-area d={curve.fill} fill={`url(#${gradient})`}/> : null}<path data-curve d={curve.line} fill="none" stroke="currentColor" strokeWidth="4" strokeLinecap="round" strokeLinejoin="round"/></g>)}
        {sorted.map((point, index) => <circle data-measurement data-latest={index === sorted.length - 1 ? '' : undefined} key={index} cx={x(positions[index]!)} cy={y(point.numeric)} r={index === sorted.length - 1 ? 9 : 6} fill="currentColor" className="growth-curve__point"><title>{dateLabel(point.time)} · {point.value} {unit}</title></circle>)}
        {last ? <circle cx={x(positions[positions.length - 1]!)} cy={y(last.numeric)} r="16" fill="none" stroke="currentColor" strokeWidth="8" strokeOpacity=".22"/> : null}
        {last && standards.length > 0 && reference?.comparisonAllowed && comparisonLabel ? <g data-who-difference transform={`translate(${Math.max(0, x(positions[positions.length - 1]!) - 130)} ${Math.max(12, y(last.numeric) - 62)})`}><rect width="118" height="42" rx="10" fill="#0e1210"/><text x="59" y="28" textAnchor="middle" className="growth-curve__callout">{comparisonLabel}</text></g> : null}
        {standards.length ? <g className="growth-curve__who-labels">{(['p97', 'p50', 'p3'] as const).map(boundary => <text key={boundary} x="850" y={y(standards[standards.length - 1]![boundary]) + 6}>{boundary.toUpperCase()}</text>)}</g> : null}
        <g data-axis="x">{ticks.map((tick, index) => <text key={index} x={x(tick)} y="409" textAnchor={ticks.length === 1 ? 'middle' : index === 0 ? 'start' : index === ticks.length - 1 ? 'end' : 'middle'}>{ageAxis ? `${Number(tick.toFixed(1))} 月龄` : dateLabel(tick)}</text>)}</g>
    </svg>;
});
