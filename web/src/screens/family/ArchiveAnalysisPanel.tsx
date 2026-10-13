import type { ArchiveAnalysis } from '../../types/archives';
import { TrendPlot, chartDay } from './TrendPlot';
import { factLabel } from './archiveLabels';
type Group = ArchiveAnalysis['groups'][number];
/** Only SVG coordinates use binary floats. Every printed value is Core's
 * exact decimal string; incompatible units, currencies and samples stay apart. */
export function ArchiveAnalysisPanel({ analysis, dateLabel, timezone = 'UTC' }: {
    analysis: ArchiveAnalysis;
    dateLabel: (value: string) => string;
    timezone?: string;
}) {
    const buckets = new Map<string, Group[]>();
    for (const group of analysis.groups) {
        const key = JSON.stringify([group.metric, group.unit, group.currency, group.sample]);
        const values = buckets.get(key) ?? [];
        values.push(group);
        buckets.set(key, values);
    }
    return <section className="archive-summary" aria-label="公开档案分析">
  <h2 className="page__section-title">公开档案分析</h2>
  {Array.from(buckets.entries()).map(([key, groups]) => {
            const first = groups[0];
            if (!first)
                return null;
            const points = groups.filter(g => g.date && Number.isFinite(Number(g.value)));
            return <article className="family-trend" key={key}>
    <h3>{factLabel(first.metric)} {first.currency ?? first.unit ?? ''}</h3>
    {first.sample ? <p className="muted">样本：{first.sample}</p> : null}
    <TrendPlot label={`${factLabel(first.metric)}趋势，精确数值见下方`} unit={first.currency ?? first.unit ?? ''} points={points.map(g => ({time: chartDay(g.date!, timezone), value: g.value}))} formatTime={time => new Intl.DateTimeFormat('zh-CN', {timeZone: 'UTC', year:'numeric',month:'2-digit',day:'2-digit'}).format(time)}/>

    {groups.map((g, i) => <div key={i} className="archive-analysis-value">
     {g.date ? <span className="muted">{dateLabel(g.date)} · </span> : null}
     <span className="serif">{g.value}</span> {g.currency ?? g.unit ?? (g.metric === 'documents' ? '份' : '')}
     {g.denominator ? <p>原始成绩 {g.numerator} / {g.denominator}</p> : null}
     {g.change_percent_points ? <p>较上一期变化 {g.change_percent_points} 个百分点</p> : null}
     {g.rounding ? <p className="muted">舍入规则：{roundingLabel(g.rounding)}</p> : null}
    </div>)}
   </article>;
        })}
  {!analysis.groups.length ? <p className="muted">暂无可确定统计的数值。待归属、未决识别和角色未明确的内容不计入。</p> : null}
  <p className="muted">{analysis.coverage.complete ? '当前公开档案范围内' : '部分资料待确认 · 统计尚不完整'} · 不随当前分页变化</p>
  <p className="muted">分析算法版本：{analysis.algorithm_version}</p>
 </section>;
}
function roundingLabel(rule: string) { return ({ 'half_away_from_zero/2':'保留两位小数，中点向远离零的方向舍入', half_even: '四舍六入五成双', half_up: '四舍五入', exact: '精确值，无舍入' } as Record<string, string>)[rule] ?? rule; }
