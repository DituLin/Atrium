import { useRef } from 'react';
import type { ArchiveOverview } from '../../types/archives';
import { RemoteButton } from '../../ui/RemoteButton';
import type { RemoteKey } from '../../ui/keys';
import { factLabel } from './archiveLabels';
import { DashboardPlot } from './DashboardPlot';
import { GrowthCurve } from './GrowthCurve';
import { calendarAgeLabel } from './calendarAge';
import type { CalendarDay } from './calendarAge';
import { summarizeFinance, summarizeGrowth, summarizeMilk } from './dashboardStats';
import type { DashboardSeries } from './dashboardStats';
import type { MilkPeriod, MilkTrendData } from './feedingTrend';
import type { FinanceTrendData } from './financeTrend';
import type { GrowthData } from './growthTrend';
import '../../styles/archive-dashboard.css';

export interface ArchiveDashboardProps {
    overview: ArchiveOverview | null;
    milk: MilkTrendData | null;
    milkPeriod: MilkPeriod;
    growth: GrowthData | null;
    finance: FinanceTrendData | null;
    personName: string | null;
    personID?: string;
    scopeName?: string;
    domain?: string;
    ownership?: string;
    busy: boolean;
    timezone: string;
    currentDay?: CalendarDay | null;
    onMilkPeriod: (period: MilkPeriod) => void;
    onLeft: () => void;
    onUp: () => void;
    onDown: () => void;
    onRead: () => void;
}
const periods: { id: MilkPeriod; label: string }[] = [{ id: 'day', label: '日' }, { id: 'week', label: '周' }, { id: 'month', label: '月' }];
const domains = [{ id: 'health', name: '健康' }, { id: 'finance', name: '财务' }, { id: 'education', name: '教育' }, { id: 'property', name: '房屋' }];
const decimal = (value: string | null) => value === null ? '—' : value.replace(/^([+-]?)(\d+)(.*)$/, (_all, sign: string, integer: string, fraction: string) => `${sign}${integer.replace(/\B(?=(\d{3})+(?!\d))/g, ',')}${fraction}`);
const signed = (value: string) => /^[+-]/.test(value) || /^0(?:\.0*)?$/.test(value) ? value : `+${value}`;
const percent = (value: string) => `${signed(value.replace(/%$/, ''))}%`;
function shortDate(value: string | undefined, timezone: string, monthOnly = false): string {
    if (!value) return '';
    const literal = /^\d{4}-\d{2}(?:-\d{2})?$/.test(value);
    const time = Date.parse(value.length === 7 ? `${value}-01` : value);
    if (!Number.isFinite(time)) return value;
    return new Intl.DateTimeFormat('zh-CN', { timeZone: literal ? 'UTC' : timezone, month: 'numeric', ...(monthOnly ? {} : { day: 'numeric' }) }).format(time);
}

export function ArchiveDashboard({ overview, milk, milkPeriod, growth, finance, personName, personID, scopeName, domain, ownership, busy, timezone, currentDay, onMilkPeriod, onLeft, onUp, onDown, onRead }: ArchiveDashboardProps) {
    const milkName = scopeName ?? personName ?? '全家';
    const personProfile = personID ? overview?.growth_profiles?.find(profile => profile.entity_id === personID) ?? null : null;
    const milkAge = personProfile && !ownership ? calendarAgeLabel(personProfile.birth_date, currentDay) : null;
    const milkTitle = milkAge ? `奶量 - ${milkAge}` : personID ? '奶量' : `奶量 · ${milkName}`;
    const periodButtons = useRef<(HTMLButtonElement | null)[]>([]);
    const dashboard = useRef<HTMLElement>(null);
    const focusGrowth = () => dashboard.current?.querySelector<HTMLButtonElement>('.growth-curve__switch button[aria-pressed="true"]')?.focus();
    const cards = useRef<(HTMLButtonElement | null)[]>([]);
    const milkSeries = ownership ? [] : summarizeMilk(milk, milkPeriod);
    const height = ownership ? [] : summarizeGrowth(growth, 'height', timezone);
    const weight = ownership ? [] : summarizeGrowth(growth, 'weight', timezone);
    const financeSeries = ownership ? [] : summarizeFinance(finance);
    const composition = domains.map(d => ({ ...d, count: (overview?.domains ?? []).filter(item => item.domain === d.id).reduce((sum, item) => sum + item.count, 0) }));
    const otherCount = (overview?.domains ?? []).filter(item => !domains.some(d => d.id === item.domain)).reduce((sum, item) => sum + item.count, 0);
    if (otherCount) composition.push({ id: 'other', name: '其他', count: otherCount });
    const coveredCount = composition.reduce((sum, item) => sum + item.count, 0);
    const uncoveredCount = Math.max(0, (overview?.total ?? 0) - coveredCount);
    const inconsistentComposition = !!overview && coveredCount > overview.total;
    const domainDimmed = (id: string) => !!domain && (id === 'other' ? domains.some(d => d.id === domain) : domain !== id);
    const empty = (kind: 'milk' | 'growth' | 'finance') => ownership ? '所选归属不纳入确定统计' : busy ? '正在核验记录…' : kind === 'growth' && !personName ? '选择一位家人查看成长记录' : kind === 'milk' ? milk ? '所选时间范围暂无奶量记录' : '奶量统计暂不可用' : kind === 'finance' ? finance ? '本月暂无已确认金额' : '财务金额统计暂不可用' : growth ? '暂无日期、数值和单位完整的测量记录' : '成长记录暂不可用';
    const cardDirection = (index: number, key: RemoteKey) => {
        if (key === 'up') { if (index === 0) onUp(); else focusGrowth(); }
        if (key === 'down') { if (index === 0) focusGrowth(); else onDown(); }
        if (key === 'left') { if (index === 0) periodButtons.current[2]?.focus(); else if (index === 1) onLeft(); else cards.current[index - 1]?.focus(); }
        if (key === 'right') { if (index === 0 || index === 3) onRead(); else cards.current[index + 1]?.focus(); }
    };
    const mini = (metric: 'height' | 'weight' | 'finance', title: string, series: DashboardSeries[], index: number) => <RemoteButton ref={el => { cards.current[index] = el; }} className={`archive-dashboard__card archive-dashboard__mini archive-dashboard__mini--${metric}`} key={metric} onClick={onRead} onDirection={key => cardDirection(index, key)} aria-label={`${title}${metric !== 'finance' && personName ? ` · ${personName}` : ''}`}>
        <div className="archive-dashboard__card-heading"><h3><span className="archive-dashboard__dot"/>{title}{metric !== 'finance' && personName ? ` · ${personName}` : ''}</h3><span className="archive-dashboard__when">{shortDate(metric === 'finance' ? finance?.months[finance.months.length - 1] : series[0]?.latestDate, timezone, metric === 'finance')}</span></div>
        {!series.length ? <><div className="archive-dashboard__number"><strong>—</strong><span>{metric === 'height' ? 'cm' : metric === 'weight' ? 'kg' : ''}</span></div><p className="archive-dashboard__empty">{empty(metric === 'finance' ? 'finance' : 'growth')}</p><div className="archive-dashboard__mini-space" aria-hidden="true"/></> : series.map(group => <div className="archive-dashboard__series" key={group.key}>
            {series.length > 1 || group.sample ? <p className="archive-dashboard__series-label">{[group.sample, metric === 'finance' ? group.currency || group.unit : series.length > 1 ? group.unit : null].filter(Boolean).join(' · ')}</p> : null}
            <div className="archive-dashboard__number"><strong>{decimal(group.headline)}</strong><span>{group.currency || group.unit}</span></div>
            <div className="archive-dashboard__comparison">{metric === 'finance' && group.changePercent !== null ? <><span className="archive-dashboard__badge">{percent(group.changePercent)}</span><span>较上月 · 已确认金额</span></> : group.change !== null ? <><span className="archive-dashboard__badge">{signed(group.change)} {group.currency || group.unit}</span><span>{metric === 'finance' ? '较上月 · 已确认金额' : '较上次测量'}</span></> : <span>{metric === 'finance' ? group.headline === null ? '本月暂无已确认金额' : '本月 · 已确认金额' : '最近一次测量'}</span>}</div>
            {group.points.length ? <DashboardPlot mini points={group.points} label={`${title}${personName && metric !== 'finance' ? ` · ${personName}` : ''}，${group.points.length} 条记录`} unit={group.currency || group.unit} timezone={timezone}/> : <div className="archive-dashboard__mini-space" aria-hidden="true"/>}
            {group.points.length ? <div className="archive-dashboard__range"><span>{shortDate(group.points[0]?.date, timezone, metric === 'finance')}</span><span>{shortDate(group.points[group.points.length - 1]?.date, timezone, metric === 'finance')}</span></div> : null}
            {!group.complete ? <p className="archive-dashboard__coverage">部分记录待确认或超出统计范围</p> : null}
        </div>)}
    </RemoteButton>;
    return <section ref={dashboard} className="archive-dashboard" aria-label="公开档案分析">
        <div className="archive-dashboard__grid">
            <article className="archive-dashboard__card archive-dashboard__milk" aria-label="奶量趋势与时间范围">
                <div className="archive-dashboard__card-heading"><h3><span className="archive-dashboard__dot"/>{milkTitle}</h3><div className="milk-period archive-dashboard__period" aria-label="奶量时间范围">{periods.map((p, i) => <RemoteButton ref={el => { periodButtons.current[i] = el; }} key={p.id} className="btn" aria-pressed={milkPeriod === p.id} onClick={() => onMilkPeriod(p.id)} onDirection={key => {
                    if (key === 'left') { if (i) periodButtons.current[i - 1]?.focus(); else onLeft(); }
                    if (key === 'right') { if (i < 2) periodButtons.current[i + 1]?.focus(); else cards.current[0]?.focus(); }
                    if (key === 'up') onUp(); if (key === 'down') focusGrowth();
                }}>{p.label}</RemoteButton>)}</div></div>
                {!milkSeries.length ? <><div className="archive-dashboard__number archive-dashboard__number--large"><strong>—</strong><span>ml</span></div><p className="archive-dashboard__caption">{empty('milk')}</p><div className="archive-dashboard__milk-space" aria-hidden="true"/></> : milkSeries.map(group => <div className="archive-dashboard__series" key={group.key}>
                    {milkSeries.length > 1 || group.sample ? <p className="archive-dashboard__series-label">{[milkSeries.length > 1 ? factLabel(group.metric) : null, group.sample].filter(Boolean).join(' · ')}</p> : null}
                    <div className="archive-dashboard__number archive-dashboard__number--large"><strong>{decimal(group.headline)}</strong><span>{group.unit}</span>{milkPeriod === 'day' ? <span className="archive-dashboard__badge">{group.count} 次</span> : group.changePercent !== null ? <span className="archive-dashboard__badge">{percent(group.changePercent)}</span> : null}</div>
                    <p className="archive-dashboard__caption">{milkPeriod === 'day' ? '今天合计 · 每次喂奶' : `近 ${milkPeriod === 'week' ? '7' : '30'} 天 · 有记录日均`}{milkPeriod === 'day' && group.latestDate && group.latestValue ? ` · 最新 ${group.latestDate.includes('T') ? new Intl.DateTimeFormat('zh-CN', { timeZone: timezone, hour: '2-digit', minute: '2-digit', hour12: false }).format(Date.parse(group.latestDate)) : shortDate(group.latestDate, timezone)} · ${group.latestValue} ${group.unit}` : milkPeriod !== 'day' ? ` · ${group.recorded}/${group.expected} 天有记录` : ''}</p>
                    <div className="archive-dashboard__milk-chart"><DashboardPlot points={group.points} label={`${milkTitle}，${milkPeriod === 'day' ? '每次记录' : '每日总量'}，${group.count} 条记录`} unit={group.unit} timezone={timezone} period={milkPeriod} dates={milk?.dates} zero/></div>
                    {!group.complete ? <p className="archive-dashboard__coverage">部分资料待确认，当前统计尚不完整</p> : null}
                </div>)}
            </article>
            <RemoteButton ref={el => { cards.current[0] = el; }} className="archive-dashboard__card archive-dashboard__mix" onClick={onRead} onDirection={key => cardDirection(0, key)} aria-label="档案构成 · 全家公开范围">
                <div className="archive-dashboard__card-heading"><h3><span className="archive-dashboard__dot"/>档案构成</h3></div>
                <div className="archive-dashboard__number archive-dashboard__number--large"><strong>{overview ? overview.total.toLocaleString('zh-CN') : '—'}</strong><span>份</span></div>
                <p className="archive-dashboard__caption">{overview ? `${overview.confirmed} 份已确认 · ${overview.pending} 份待确认` : busy ? '正在核验档案构成…' : '档案构成暂不可用'}</p>
                <div className="archive-dashboard__composition" aria-hidden="true">{!inconsistentComposition ? <>{composition.map(d => <span key={d.id} className={`archive-dashboard__segment archive-dashboard__domain--${d.id}`} data-dimmed={domainDimmed(d.id)} style={{ flexGrow: d.count }}/>) }{uncoveredCount ? <span className="archive-dashboard__segment archive-dashboard__segment--uncovered" style={{ flexGrow: uncoveredCount }}/> : null}</> : null}</div>
                <div className="archive-dashboard__domains">{composition.map(d => <div key={d.id} className={`archive-dashboard__domain archive-dashboard__domain--${d.id}`} data-domain={d.id} data-dimmed={domainDimmed(d.id)}><span className="archive-dashboard__domain-dot"/><span className="archive-dashboard__domain-name">{d.name}</span><strong>{overview ? d.count.toLocaleString('zh-CN') : '—'}</strong><span className="archive-dashboard__percent">{overview && !inconsistentComposition ? `${overview.total ? Math.round(d.count / overview.total * 100) : 0}%` : '—'}</span></div>)}</div>
                {uncoveredCount ? <p className="archive-dashboard__coverage">另有 {uncoveredCount} 份尚未计入领域构成</p> : null}
                {inconsistentComposition ? <p className="archive-dashboard__coverage">领域数量尚待核验，暂不展示占比</p> : null}
                {overview && !overview.coverage.complete ? <p className="archive-dashboard__coverage">档案统计尚未完整核验</p> : null}
            </RemoteButton>
            <GrowthCurve key={personID??'household'} data={growth} personName={personName} profile={personProfile} timezone={timezone} busy={busy} ownership={ownership} onUp={()=>periodButtons.current[milkPeriod==='day'?0:milkPeriod==='week'?1:2]?.focus()} onDown={()=>cards.current[1]?.focus()} onLeft={onLeft} onRead={onRead}/>
            {mini('height', '身高', height, 1)}
            {mini('weight', '体重', weight, 2)}
            {mini('finance', '财务金额', financeSeries, 3)}
        </div>
        <p className="archive-dashboard__footnote">奶量按当前筛选 · 成长按当前人物 · 构成与财务按全家公开档案 · 不随当前分页变化</p>
    </section>;
}
