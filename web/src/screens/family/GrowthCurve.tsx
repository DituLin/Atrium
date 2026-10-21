import { useMemo, useRef, useState } from 'react';
import { RemoteButton } from '../../ui/RemoteButton';
import { chartDay } from './TrendPlot';
import { GrowthCurvePlot } from './GrowthCurvePlot';
import { buildGrowthCurve } from './growthCurveData';
import type { GrowthCurveSeries } from './growthCurveData';
import type { GrowthData, GrowthMetric } from './growthTrend';
import type { GrowthReferenceProfile } from './whoGrowth';
import { buildWhoReference } from './whoGrowth';
import '../../styles/growth-curve.css';

export interface GrowthCurveProps {
    data: GrowthData | null; personName: string | null; timezone: string; busy: boolean; ownership?: string;
    profile?: GrowthReferenceProfile | null;
    onUp: () => void; onDown: () => void; onLeft: () => void; onRead: () => void;
}

const metrics: { id: GrowthMetric; label: string }[] = [{ id: 'height', label: '身高' }, { id: 'weight', label: '体重' }];
const signed = (value: string | null, unit: string) => value === null ? '—' : `${/^[+-]/.test(value) || /^0(?:\.0*)?$/.test(value) ? value : `+${value}`} ${unit}`;
const referenceNumber = (value: number | null) => value === null ? null : String(Number(value.toFixed(2)));
function dateLabel(value: string | null, timezone: string, includeYear = false): string {
    if (!value) return '—';
    const time = chartDay(value, timezone);
    if (!Number.isFinite(time)) return '—';
    const date = new Date(time);
    return `${includeYear ? `${date.getUTCFullYear()}年` : ''}${date.getUTCMonth() + 1}月${date.getUTCDate()}日`;
}

/** The full-width growth view shows evidence only; standards require a verified profile. */
export function GrowthCurve({ data, personName, timezone, busy, ownership, profile = null, onUp, onLeft, onRead }: GrowthCurveProps) {
    const [metric, setMetric] = useState<GrowthMetric>('height');
    const buttons = useRef<(HTMLButtonElement | null)[]>([]);
    const title = metric === 'height' ? '身高' : '体重';
    const unit = metric === 'height' ? 'cm' : 'kg';
    const groups = useMemo(() => {
        const series = ownership || !personName ? [] : buildGrowthCurve(data, metric, timezone);
        if (!series.length && profile && personName && data && !ownership) {
            const placeholder: GrowthCurveSeries = { key: `${metric}-reference-only`, metric, unit, points: [], latestValue: null, latestDate: null, previousChange: null, recentChange: null, recentFrom: null, recentTo: null, complete: !data.limited.includes(metric) && data[metric].every(result => result.coverage.complete), count: 0 };
            series.push(placeholder);
        }
        return series.map(group => ({ ...group, reference: profile ? buildWhoReference(group, metric, profile, timezone) : null }));
    }, [data, metric, ownership, personName, profile, timezone, unit]);
    const hasReference = groups.some(group => group.reference?.points.length);
    const referenceNotes = [...new Set(groups.map(group => group.reference?.note).filter((note): note is string => !!note))];
    const empty = ownership ? '所选归属不纳入确定统计' : !personName ? '选择一位家人查看生长曲线' : busy ? '正在核验成长记录…' : !data ? '生长曲线暂不可用，请刷新重试' : data.limited.includes(metric) ? `${title}记录超出统计范围，暂不能完整展示` : `暂无日期、数值和单位完整的${title}记录`;
    return <article className="growth-curve" aria-label="生长曲线">
        <div className="growth-curve__heading">
            <h3><span className="growth-curve__dot"/>生长曲线{personName ? ` · ${personName}` : ''}</h3>
            <div className="growth-curve__legend" aria-label="图例"><span className="growth-curve__legend-entry"><span className="growth-curve__legend-line"/>实测</span>{hasReference ? <><span className="growth-curve__legend-entry"><span className="growth-curve__legend-median"/>WHO 标准中位数</span><span className="growth-curve__legend-entry"><span className="growth-curve__legend-band"/>P3–P97 参考区间</span></> : null}</div>
            <div className="growth-curve__switch" aria-label="生长曲线指标">{metrics.map((item, index) => <RemoteButton className="btn" key={item.id} ref={element => { buttons.current[index] = element; }} aria-pressed={metric === item.id} onClick={() => setMetric(item.id)} onDirection={key => {
                if (key === 'up') onUp();
                if (key === 'down') onRead();
                if (key === 'left') { if (index) buttons.current[index - 1]?.focus(); else onLeft(); }
                if (key === 'right') { if (index < metrics.length - 1) buttons.current[index + 1]?.focus(); else onRead(); }
            }}>{item.label}</RemoteButton>)}</div>
        </div>
        {!groups.length ? <div className="growth-curve__series growth-curve__series--empty"><div className="growth-curve__summary"><div className="growth-curve__latest"><strong>—</strong><span>{unit}</span></div><p className="growth-curve__caption">{empty}</p></div><div className="growth-curve__empty-plot" aria-hidden="true"><span/><span/><span/><span/><span/></div></div> : groups.map(group => <div className="growth-curve__series" key={group.key}>
            <div className="growth-curve__summary">
                {groups.length > 1 || group.sample ? <p className="growth-curve__series-label">{[group.sample, group.unit].filter(Boolean).join(' · ')}</p> : null}
                <div className="growth-curve__latest"><strong>{group.latestValue ?? '—'}</strong><span>{group.unit}</span></div>
                <p className="growth-curve__caption">{(group.complete || group.reference?.ageIsCurrent) && group.reference?.latestAgeMonths !== null && group.reference?.latestAgeMonths !== undefined ? `${group.reference.ageIsCurrent ? '当前约' : ''}${Number(group.reference.latestAgeMonths.toFixed(1))} 月龄 · ` : ''}{group.latestDate ? `最新测量 ${dateLabel(group.latestDate, timezone)}` : group.count === 0 && group.complete ? '暂无实测记录' : '最新测量尚待完整核验'}</p>
                {group.reference?.latestWithinRange !== null && group.reference?.latestWithinRange !== undefined ? <span className="growth-curve__badge" data-within-range={group.reference.latestWithinRange}>{group.reference.latestWithinRange ? 'P3–P97 参考区间内' : 'P3–P97 参考区间外'}</span> : <span className="growth-curve__badge">{group.count} 次实测</span>}
                <dl className="growth-curve__stats">
                    {group.reference?.points.length ? <><div><dt>与标准中位数</dt><dd className="growth-curve__difference">{signed(referenceNumber(group.reference.medianDifference), group.unit)}</dd></div><div><dt>同龄百分位</dt><dd>{group.reference.percentile === null ? '—' : `P${Number(group.reference.percentile.toFixed(1))}`}</dd></div></> : <div><dt>较上次测量</dt><dd>{signed(group.previousChange, group.unit)}</dd></div>}
                    <div><dt>近 3 个月已记录增长</dt><dd>{signed(group.recentChange, group.unit)}</dd></div>
                    {group.reference?.points.length ? <div><dt>标准同期增长</dt><dd className="growth-curve__standard-change">{signed(referenceNumber(group.reference.recentMedianChange), group.unit)}</dd></div> : null}
                </dl>
                {group.recentFrom && group.recentTo ? <p className="growth-curve__interval">实测区间 {dateLabel(group.recentFrom, timezone)} — {dateLabel(group.recentTo, timezone)}</p> : null}
                {!group.complete ? <p className="growth-curve__coverage">部分记录尚未完整核验，暂不计算最新值与增长</p> : group.count === 1 ? <p className="growth-curve__coverage">仅有 1 次测量，暂未形成连续曲线</p> : null}
            </div>
            <div className="growth-curve__chart">{group.points.length || group.reference?.points.length ? <GrowthCurvePlot points={group.points} label={`生长曲线 · ${personName} · ${title}，${group.count} 条实测`} unit={group.unit} timezone={timezone} reference={group.reference} birthDate={profile?.birth_date}/> : <div className="growth-curve__empty-plot" aria-hidden="true"><span/><span/><span/><span/><span/></div>}</div>
        </div>)}
        <p className="growth-curve__reference">{referenceNotes.length ? referenceNotes.join(' · ') : profile ? 'WHO 标准参考将在可用记录核验后显示。' : 'WHO 标准参考：缺少出生日期与性别，暂不计算月龄、标准范围或百分位。'}</p>
    </article>;
}
