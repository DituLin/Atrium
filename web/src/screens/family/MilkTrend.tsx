import { useRef } from 'react';
import { TrendPlot, chartTime } from './TrendPlot';
import { RemoteButton } from '../../ui/RemoteButton';
import type { MilkPeriod, MilkTrendData } from './feedingTrend';
import { factLabel } from './archiveLabels';

const periods: { id: MilkPeriod; label: string }[] = [{ id: 'day', label: '日' }, { id: 'week', label: '周' }, { id: 'month', label: '月' }];
export function MilkTrend({ data, period, busy, timezone, onPeriod, onLeft, onUp, onDown, onRead }: {
    data: MilkTrendData | null; period: MilkPeriod; busy: boolean; timezone: string;
    onPeriod: (period: MilkPeriod) => void; onLeft: () => void; onUp: () => void; onDown: () => void; onRead: () => void;
}) {
    const buttons = useRef<(HTMLButtonElement | null)[]>([]);
    const series = new Map<string, MilkTrendData['groups']>();
    for (const group of data?.groups ?? []) {
        if (!group.date || !group.unit || group.currency || !Number.isFinite(Number(group.value)) || !Number.isFinite(chartTime(group.date))) continue;
        const key = JSON.stringify([group.metric, group.unit, group.sample]);
        const values = series.get(key) ?? [];
        values.push(group); series.set(key, values);
    }
    const dateLabel = (date: string) => new Intl.DateTimeFormat('zh-CN', period === 'day' ? { hour: '2-digit', minute: '2-digit', timeZone: timezone } : { month: 'numeric', day: 'numeric', timeZone: 'UTC' }).format(Date.parse(date));
    return <section className="milk-trend" aria-label="奶量趋势与时间范围">
        <div className="family-section-heading"><h2 className="page__section-title">奶量趋势</h2><div className="milk-period" aria-label="奶量时间范围">
            {periods.map((p, i) => <RemoteButton key={p.id} ref={el => { buttons.current[i] = el; }} className="btn" aria-pressed={period === p.id} onClick={() => onPeriod(p.id)} onDirection={key => {
                if (key === 'left') {
                    if (i) buttons.current[i - 1]?.focus();
                    else onLeft();
                }
                if (key === 'right') {
                    if (i < periods.length - 1) buttons.current[i + 1]?.focus();
                    else onRead();
                }
                if (key === 'up') onUp(); if (key === 'down') onDown();
            }}>{p.label}</RemoteButton>)}
        </div></div>
        <p className="muted">{period === 'day' ? '今天 · 每次喂奶' : period === 'week' ? '近 7 天 · 每日总奶量' : '近 30 天 · 每日总奶量'} · 仅统计已归档记录{data && !data.complete ? ' · 部分资料待确认' : ''}</p>
        {!data ? <p className="muted">{busy ? '正在核验奶量…' : '奶量统计暂不可用，请刷新重试。'}</p> : !series.size ? <p className="muted">所选时间范围暂无可统计的奶量记录。</p> : Array.from(series.entries()).map(([key, groups]) => {
            const points = [...groups].sort((a, b) => Date.parse(a.date!) - Date.parse(b.date!));
            const latest = points[points.length - 1];
            const first = points[0];
            if (!first || !latest || !data.dates.length) return null;
            return <article className="family-trend" key={key}>
                <h3>{factLabel(latest.metric)}</h3>
                <div className="family-trend__value serif">{latest.value}<small>{latest.unit}</small></div>
                <p className="muted">{period === 'day' ? '最新一次' : '最近有记录的一天'} · {dateLabel(latest.date!)}{latest.sample ? ` · ${latest.sample}` : ''}</p>
                <TrendPlot label={`奶量趋势，${points.length} ${period === 'day' ? '次记录' : '天有记录'}，最新 ${latest.value} ${latest.unit}`} unit={latest.unit ?? ''} zero points={points.map(p => ({ time: chartTime(p.date!), value: p.value }))} formatTime={time => dateLabel(new Date(time).toISOString())} range={period === 'day' ? undefined : [chartTime(data.dates[0]!), chartTime(data.dates[data.dates.length - 1]!)]}/>

            </article>;
        })}
    </section>;
}
