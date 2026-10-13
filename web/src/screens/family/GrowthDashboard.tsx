import { useRef } from 'react';
import { RemoteButton } from '../../ui/RemoteButton';
import type { ArchiveAnalysis } from '../../types/archives';
import type { GrowthData, GrowthMetric, GrowthPeriod } from './growthTrend';
import { TrendPlot, chartTime, chartDay } from './TrendPlot';
const periods: { id: GrowthPeriod; label: string }[] = [{ id: 'quarter', label: '近 3 个月' }, { id: 'year', label: '近 1 年' }, { id: 'all', label: '全部阶段' }];
export function GrowthDashboard({ data, name, period, busy, timezone, onPeriod, onLeft, onUp, onDown, onRead }: {
    data: GrowthData | null; name: string; period: GrowthPeriod; busy: boolean; timezone: string;
    onPeriod: (period: GrowthPeriod) => void; onLeft: () => void; onUp: () => void; onDown: () => void; onRead: () => void;
}) {
    const buttons = useRef<(HTMLButtonElement | null)[]>([]);
    const formatDate = (value: string) => /^\d{4}-\d{2}-\d{2}$/.test(value) ? value.replace(/-/g, '/') : new Intl.DateTimeFormat('zh-CN', { timeZone: timezone, year: 'numeric', month: '2-digit', day: '2-digit' }).format(chartTime(value));
    const calendarTime = (value: string) => chartDay(value, timezone);
    return <section className="growth-dashboard" aria-label={`${name}成长趋势`}>
        <div className="family-section-heading"><div><p className="growth-eyebrow">成长档案</p><h2 className="page__section-title">{name} · 成长趋势</h2></div>
            <div className="growth-period" aria-label="成长时间范围">{periods.map((p, i) => <RemoteButton key={p.id} ref={el => { buttons.current[i] = el; }} className="btn" aria-pressed={period === p.id} onClick={() => onPeriod(p.id)} onDirection={key => {
                if (key === 'left') { if (i) buttons.current[i - 1]?.focus(); else onLeft(); }
                if (key === 'right') { if (i < periods.length - 1) buttons.current[i + 1]?.focus(); else onRead(); }
                if (key === 'up') onUp(); if (key === 'down') onDown();
            }}>{p.label}</RemoteButton>)}</div>
        </div>
        <p className="muted growth-caption">每次测量，连成成长的轨迹 · 按实际测量日期展示</p>
        <div className="growth-charts">{(['height', 'weight'] as GrowthMetric[]).map(metric => {
            const title = metric === 'height' ? '身高' : '体重';
            const buckets = new Map<string, ArchiveAnalysis['groups']>();
            let undated = 0;
            for (const result of data?.[metric] ?? []) for (const group of result.groups) {
                if (!group.date || !Number.isFinite(chartTime(group.date)) || !group.value.trim() || !Number.isFinite(Number(group.value)) || !group.unit || group.currency) { undated++; continue; }
                const key = JSON.stringify([group.unit, group.sample]);
                const points = buckets.get(key) ?? []; points.push(group); buckets.set(key, points);
            }
            return <article className={`family-trend growth-card growth-card--${metric}`} key={metric}>
                <div className="family-trend__label"><h3>{title}</h3><span className="muted">{metric === 'height' ? 'HEIGHT' : 'WEIGHT'}</span></div>
                {!data ? <p className="muted">{busy ? '正在核验成长记录…' : '暂时无法读取，请刷新重试。'}</p> : null}
                {data?.limited.includes(metric) ? <p className="family-message">部分{title}记录超出统计范围，请切换更短时间范围。</p> : null}
                {data && !buckets.size ? <div className="growth-empty"><span className="serif">—</span><p>暂无日期、数值和单位完整的{title}记录</p><p>后续测量归档后会自动更新。</p></div> : null}
                {[...buckets.entries()].map(([key, groups]) => {
                    const points = [...groups].sort((a, b) => calendarTime(a.date!) - calendarTime(b.date!) || chartTime(a.date!) - chartTime(b.date!));
                    const latest = points[points.length - 1]!;
                    return <div className="growth-series" key={key}>
                        {latest.sample ? <p className="muted">样本：{latest.sample}</p> : null}
                        <div className="family-trend__value serif">{latest.value}<small>{latest.unit}</small></div>
                        <p className="family-trend__change muted">最近测量 {formatDate(latest.date!)} · {points.length} 次记录</p>
                        <TrendPlot label={`${name}${title}趋势，${points.length} 次记录，最新 ${latest.value} ${latest.unit}`} unit={latest.unit!} points={points.map(p => ({time: calendarTime(p.date!), value: p.value}))} formatTime={time => new Intl.DateTimeFormat('zh-CN', {timeZone:'UTC',year:'numeric',month:'2-digit',day:'2-digit'}).format(time)}/>
                        <table className="growth-measurements"><caption>最近 {Math.min(points.length, 3)} 次测量 · 曲线展示所选范围全部记录</caption><thead><tr><th>测量日期</th><th>{title}（{latest.unit}）</th></tr></thead><tbody>{points.slice(-3).reverse().map((p, i) => <tr key={i}><td>{formatDate(p.date!)}</td><td>{p.value}</td></tr>)}</tbody></table>
                    </div>;
                })}
                {undated ? <p className="muted">另有 {undated} 条记录缺少明确日期或数值单位，暂未绘入曲线。</p> : null}
                {data?.[metric].some(a => !a.coverage.complete) ? <p className="muted">部分资料待确认，当前曲线尚不完整。</p> : null}
            </article>;
        })}</div>
    </section>;
}
