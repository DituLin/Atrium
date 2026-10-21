import { useId } from 'react';
import { chartDay, chartTime } from './TrendPlot';
import { trendSegments } from './trendCurve';
import type { MilkPeriod } from './feedingTrend';

export interface DashboardPlotPoint { date: string; value: string }
export interface DashboardPlotProps {
    points: readonly DashboardPlotPoint[];
    label: string;
    unit: string;
    timezone: string;
    period?: MilkPeriod;
    dates?: readonly string[];
    mini?: boolean;
    zero?: boolean;
}
const tickValue = (value: number) => Math.abs(value) >= 1e7 ? value.toExponential(1) : Number(value.toPrecision(6)).toLocaleString('zh-CN', { maximumFractionDigits: 6 });

/** Compact TV charts retain literal dates and plot instants in the household day. */
export function DashboardPlot({ points, label, unit, timezone, period, dates, mini = false, zero = false }: DashboardPlotProps) {
    const gradient = `dashboard-${useId().replace(/:/g, '')}`;
    const valid = points.filter(p => (period !== 'day' || p.date.includes('T')) && Number.isFinite(chartTime(p.date)) && p.value.trim() !== '' && Number.isFinite(Number(p.value)) && Math.abs(Number(p.value)) <= Number.MAX_SAFE_INTEGER);
    const minuteOfDay = (date: string) => {
        const parts = new Intl.DateTimeFormat('en-GB', { timeZone: timezone, hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false }).formatToParts(chartTime(date));
        const part = (type: string) => Number(parts.find(p => p.type === type)?.value ?? '0');
        return (part('hour') % 24) * 60 + part('minute') + part('second') / 60;
    };
    const numeric = valid.map(p => ({ ...p, time: period === 'day' ? minuteOfDay(p.date) : chartDay(p.date, timezone), numeric: Number(p.value) })).sort((a, b) => a.time - b.time || chartTime(a.date) - chartTime(b.date));
    const first = numeric[0], last = numeric[numeric.length - 1];
    if (!first || !last) return null;
    const values = numeric.map(p => p.numeric);
    const low = Math.min(...values), high = Math.max(...values);
    const pad = Math.max((high - low) * .12, Math.abs(high) * .02, .01);
    const bottom = zero && low >= 0 ? 0 : low - pad;
    const rawTop = high + pad;
    const rawStep = (rawTop - bottom) / 2;
    const power = 10 ** Math.floor(Math.log10(rawStep));
    const step = [1, 2, 2.5, 5, 10].find(n => n * power >= rawStep)! * power;
    const min = mini ? bottom : Math.floor(bottom / step) * step;
    const max = mini ? rawTop : Math.max(min + step * 2, Math.ceil(rawTop / (step * 2)) * step * 2);
    const plotWidth = mini ? 370 : 744;
    const plotTop = mini ? 6 : 8;
    const plotBottom = mini ? 98 : 196;
    const range = dates?.filter(date => Number.isFinite(chartTime(date))).map(date => chartDay(date, timezone));
    const start = period === 'day' ? 0 : range?.length ? Math.min(range[0]!, first.time) : first.time;
    const end = period === 'day' ? 1440 : range?.length ? Math.max(range[range.length - 1]!, last.time) : last.time;
    const x = (time: number) => start === end ? plotWidth / 2 : (time - start) / (end - start) * plotWidth;
    const y = (value: number) => plotBottom - (value - min) / (max - min) * (plotBottom - plotTop);
    const curves = trendSegments(numeric.map(p => ({ x: x(p.time), y: y(p.numeric) })), plotBottom);
    const dateLabel = (time: number) => new Intl.DateTimeFormat('zh-CN', { timeZone: 'UTC', month: 'numeric', day: 'numeric' }).format(time);
    const ticks = period === 'day' ? [0, 360, 720, 1080, 1440] : start === end ? [start] : [start, start + (end - start) / 2, end];
    return <svg className={`archive-dashboard__plot${mini ? ' archive-dashboard__plot--mini' : ''}`} viewBox={`0 0 ${mini ? 370 : 813} ${mini ? 104 : 238}`} role="img" aria-label={label}>
        <defs><linearGradient id={gradient} x1="0" y1="0" x2="0" y2="1"><stop offset="0%" stopColor="currentColor" stopOpacity={mini ? '.45' : '.5'}/><stop offset="100%" stopColor="currentColor" stopOpacity="0"/></linearGradient></defs>
        {!mini ? <g data-axis="y">{[max, (max + min) / 2, min].map((tick, i) => <g key={i}><line x1="0" x2={plotWidth} y1={y(tick)} y2={y(tick)} className="archive-dashboard__gridline"/><text x="758" y={y(tick) + 6}>{tickValue(tick)}</text></g>)}</g> : null}
        {curves.map((curve, i) => <g key={i}>{curve.fill ? <path data-area d={curve.fill} fill={`url(#${gradient})`}/> : null}<path data-curve d={curve.line} fill="none" stroke="currentColor" strokeWidth={mini ? 3.5 : 4} strokeLinecap="round" strokeLinejoin="round"/></g>)}
        <circle data-latest cx={x(last.time)} cy={y(last.numeric)} r={mini ? 7 : 8} fill="currentColor" className="archive-dashboard__endpoint"><title>{last.date} · {last.value} {unit}</title></circle>
        {!mini ? <circle cx={x(last.time)} cy={y(last.numeric)} r="14" fill="none" stroke="currentColor" strokeOpacity=".22" strokeWidth="8"/> : null}
        {!mini ? <g data-axis="x">{ticks.map((tick, i) => <text key={i} x={x(tick)} y="235" textAnchor={i === 0 ? 'start' : i === ticks.length - 1 ? 'end' : 'middle'}>{period === 'day' ? `${tick / 60}时` : dateLabel(tick)}</text>)}</g> : null}
    </svg>;
}
