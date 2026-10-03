import type { FamilyRecord } from '../../types/family';

export const METRICS: Record<string,string> = {height:'身高',weight:'体重',head_circumference:'头围',temperature:'体温',systolic:'收缩压',diastolic:'舒张压',heart_rate:'心率',glucose:'血糖',sleep:'睡眠',sleep_hours:'睡眠',steps:'步数'};
export interface Series {metric:string;unit:string;points:{time:number;value:number}[]}
export function seriesFor(records:FamilyRecord[]):Series[]{
 const groups=new Map<string,Series>();
 for(const record of records){const time=Date.parse(record.occurred_at);if(!Number.isFinite(time))continue;
  for(const m of record.measurements??[]){if(!Number.isFinite(m.value))continue;const key=JSON.stringify([m.metric,m.unit]);let group=groups.get(key);if(!group){group={metric:m.metric,unit:m.unit,points:[]};groups.set(key,group);}group.points.push({time,value:m.value});}
 }
 return [...groups.values()].map(group=>({...group,points:group.points.sort((a,b)=>a.time-b.time)}));
}
export function shortDate(time:number,timezone:string){return new Intl.DateTimeFormat('zh-CN',{month:'numeric',day:'numeric',timeZone:timezone}).format(time);}
export function FamilyTrend({series,timezone}:{series:Series;timezone:string}){
 const points=series.points;const first=points[0];const last=points[points.length-1];if(!first||!last)return null;
 const low=Math.min(...points.map(p=>p.value));const high=Math.max(...points.map(p=>p.value));const pad=Math.max((high-low)*.2,Math.abs(high)*.01,.1);const min=low-pad;const max=high+pad;
 const x=(t:number)=>first.time===last.time?280:72+(t-first.time)/(last.time-first.time)*408;
 const y=(v:number)=>148-(v-min)/(max-min)*116;
 const label=METRICS[series.metric]??series.metric;const delta=last.value-first.value;
 return <article className="family-trend">
  <div className="family-trend__label"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 18V6M4 18h16M7 14l4-4 4 2 5-7"/></svg><span>{label}</span><span className="muted">{series.unit}</span></div>
  <div className="family-trend__value serif">{last.value}<small>{series.unit}</small></div>
  <p className="family-trend__change muted">{points.length>1?`与本段首条相比 ${delta>0?'+':''}${Number(delta.toFixed(3))} ${series.unit}`:'首次记录 · 等待下一次变化'}</p>
  <svg className="family-trend__plot" viewBox="0 0 520 195" role="img" aria-label={`${label}趋势，${points.length} 次记录，最新 ${last.value} ${series.unit}`}>
   {[min,(min+max)/2,max].map((tick,i)=><g key={i}><line x1="72" x2="480" y1={y(tick)} y2={y(tick)} className="family-trend__grid"/><text x="60" y={y(tick)+5} textAnchor="end">{Number(tick.toFixed(2))}</text></g>)}
   <polyline points={points.map(p=>`${x(p.time)},${y(p.value)}`).join(' ')} fill="none" stroke="currentColor" strokeWidth="3" strokeLinejoin="round" strokeLinecap="round"/>
   {points.map((p,i)=><circle key={i} cx={x(p.time)} cy={y(p.value)} r="4" fill="currentColor"/>)}
   <text x="72" y="182">{shortDate(first.time,timezone)}</text>{first.time!==last.time?<text x="480" y="182" textAnchor="end">{shortDate(last.time,timezone)}</text>:null}
  </svg>
 </article>;
}
