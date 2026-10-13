export interface TrendPoint { time: number; value: string }
/** Partial dates stay in the textual record; a plot must not invent a day. */
export function chartTime(value: string): number {
    if (!/^\d{4}-\d{2}-\d{2}(?:$|T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$)/.test(value)) return NaN;
    const date = value.slice(0, 10);
    const day = Date.parse(`${date}T00:00:00Z`);
    if (!Number.isFinite(day) || new Date(day).toISOString().slice(0, 10) !== date) return NaN;
    return Date.parse(value);
}
/** Daily charts preserve literal dates and map instants to the household day. */
export function chartDay(value: string, timezone: string): number {
    const time = chartTime(value);
    if (!Number.isFinite(time) || value.length === 10) return time;
    const parts = new Intl.DateTimeFormat('en', {timeZone:timezone,year:'numeric',month:'2-digit',day:'2-digit'}).formatToParts(time);
    return chartTime(`${parts.find(p => p.type === 'year')!.value}-${parts.find(p => p.type === 'month')!.value}-${parts.find(p => p.type === 'day')!.value}`);
}
export function TrendPlot({ points, label, unit, formatTime, range, zero = false }: {
    points: TrendPoint[]; label: string; unit: string; formatTime: (time: number) => string;
    range?: [number, number]; zero?: boolean;
}) {
    const sorted = points.filter(p => Number.isFinite(p.time) && p.value.trim() !== '' && Number.isFinite(Number(p.value)) && Math.abs(Number(p.value)) <= Number.MAX_SAFE_INTEGER).sort((a, b) => a.time - b.time);
    const first = sorted[0], last = sorted[sorted.length - 1];
    if (!first || !last) return null;
    const values = sorted.map(p => Number(p.value));
    const low = Math.min(...values), high = Math.max(...values);
    const pad = Math.max((high - low) * .12, Math.abs(high) * .02, .01);
    const bottom = zero && low >= 0 ? 0 : low - pad;
    const top = high + pad;
    const rawStep = (top - bottom) / 4;
    const power = 10 ** Math.floor(Math.log10(rawStep));
    const step = [1, 2, 2.5, 5, 10].find(n => n * power >= rawStep)! * power;
    const min = Math.floor(bottom / step) * step, max = Math.ceil(top / step) * step;
    const yTicks = Array.from({ length: Math.round((max - min) / step) + 1 }, (_, i) => min + i * step);
    const start = range && Number.isFinite(range[0]) ? Math.min(range[0], first.time) : first.time;
    const end = range && Number.isFinite(range[1]) ? Math.max(range[1], last.time) : last.time;
    const x = (time: number) => start === end ? 328 : 88 + (time - start) / (end - start) * 480;
    const y = (value: number) => 188 - (value - min) / (max - min) * 144;
    const xTicks = start === end ? [start] : [start, start + (end - start) / 2, end];
    const tickLabel = (v: number) => Math.abs(v) >= 1e7 ? v.toExponential(1) : Number(v.toPrecision(10)).toLocaleString('zh-CN', { maximumFractionDigits: 10 });
    return <svg className="family-trend__plot" viewBox="0 0 600 252" role="img" aria-label={label}>
        <g data-axis="y"><text x="88" y="22">{unit || '数值'}</text>
            {yTicks.map((tick, i) => <g key={i}><line className="family-trend__grid" x1="88" x2="568" y1={y(tick)} y2={y(tick)}/><text x="76" y={y(tick) + 5} textAnchor="end">{tickLabel(tick)}</text></g>)}
            <line className="family-trend__axis" x1="88" x2="88" y1="44" y2="188"/>
        </g>
        <g data-axis="x"><line className="family-trend__axis" x1="88" x2="568" y1="188" y2="188"/>
            {xTicks.map((tick, i) => <g key={i}><line className="family-trend__axis" x1={x(tick)} x2={x(tick)} y1="188" y2="195"/><text x={x(tick)} y="217" textAnchor={xTicks.length === 1 ? 'middle' : i === 0 ? 'start' : i === xTicks.length - 1 ? 'end' : 'middle'}>{formatTime(tick)}</text></g>)}
            <text x="328" y="246" textAnchor="middle">日期 / 时间</text>
        </g>
        {sorted.length > 1 ? <polyline points={sorted.map(p => `${x(p.time)},${y(Number(p.value))}`).join(' ')} fill="none" stroke="currentColor" strokeWidth="3" strokeLinejoin="round" strokeLinecap="round"/> : null}
        {sorted.map((p, i) => <circle key={i} cx={x(p.time)} cy={y(Number(p.value))} r="4" fill="currentColor"><title>{formatTime(p.time)} · {p.value} {unit}</title></circle>)}
    </svg>;
}
