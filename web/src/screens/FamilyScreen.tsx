import { useEffect, useRef, useState } from 'react';
import { useApp } from '../app/context';
import type { FamilyCategory, FamilyMember, FamilyRecordPage } from '../types/family';
import { RemoteButton } from '../ui/RemoteButton';
import { TopNav, focusTopNav } from '../ui/TopNav';
import { onReadingKey, restoreReturnFocus } from './info/readingRegion';
import { FamilyTrend, METRICS, seriesFor } from './family/FamilyTrend';

const CATEGORIES:{id:FamilyCategory|'all';label:string;icon:string}[]=[
 {id:'all',label:'全部记录',icon:'M4 5h16M4 12h16M4 19h16'},
 {id:'growth',label:'成长',icon:'M12 20V9M12 13C5 13 4 8 4 5c6 0 8 3 8 8M12 10c0-5 4-7 8-7 0 5-3 8-8 8'},
 {id:'health',label:'健康',icon:'M12 20S3 15 3 9a5 5 0 0 1 9-3 5 5 0 0 1 9 3c0 6-9 11-9 11M6 12h3l2-4 3 8 2-4h3'},
 {id:'checkup',label:'体检',icon:'M8 4H5v17h14V4h-3M8 2h8v5H8zM8 12h8M8 16h5'},
 {id:'meal',label:'饮食计划',icon:'M5 3v7M8 3v7M11 3v7M5 7h6M8 10v11M18 3v18M18 3c-4 3-4 9 0 9'},
 {id:'note',label:'生活记事',icon:'M5 3h14v18H5zM9 8h6M9 12h6M9 16h3'},
];
export function FamilyScreen(){
 const {api,state}=useApp();const [members,setMembers]=useState<FamilyMember[]>([]);const [member,setMember]=useState('');const [category,setCategory]=useState<FamilyCategory|'all'>('all');
 const [snapshot,setSnapshot]=useState<{member:string;page:FamilyRecordPage;at:number}|null>(null);const [status,setStatus]=useState<'loading'|'ready'|'failed'>('loading');const [refresh,setRefresh]=useState(0);const [expanded,setExpanded]=useState(false);const [moreBusy,setMoreBusy]=useState(false);const [moreError,setMoreError]=useState(false);
 const reading=useRef<HTMLDivElement>(null);const controls=useRef<(HTMLButtonElement|null)[]>([]);const latest=useRef(snapshot);latest.current=snapshot;
 const more=useRef<HTMLButtonElement>(null);const refreshButton=useRef<HTMLButtonElement>(null);const requestEpoch=useRef(0);const expandedRef=useRef(false);
 useEffect(()=>{restoreReturnFocus(state.router.restoreFocus,reading.current);},[state.router.restoreFocus]);
 useEffect(()=>{
  let active=true;let busy=false;const controller=new AbortController();const epoch=++requestEpoch.current;expandedRef.current=false;setExpanded(false);setStatus('loading');setMoreBusy(false);setMoreError(false);
  const load=async()=>{if(busy)return;busy=true;try{
   const result=await api.getFamilyMembers(controller.signal);if(!active||expandedRef.current)return;if(!Array.isArray(result.members))throw new Error('Invalid members response');setMembers(result.members);
   const selected=result.members.some(m=>m.id===member)?member:result.members[0]?.id??'';
   if(selected!==member){setMember(selected);setSnapshot(null);return;}
   if(!selected){setSnapshot(null);setStatus('ready');return;}
   const page=await api.getFamilyRecords(selected,'',controller.signal);if(!active||epoch!==requestEpoch.current||expandedRef.current)return;if(!Array.isArray(page.records))throw new Error('Invalid records response');setSnapshot({member:selected,page,at:Date.now()});setStatus('ready');
  }catch{if(active&&!expandedRef.current)setStatus('failed');}finally{busy=false;}};
  void load();const timer=window.setInterval(()=>{if(!expandedRef.current)void load();},15000);
  return()=>{active=false;requestEpoch.current++;controller.abort();if(timer!==undefined)window.clearInterval(timer);};
 },[api,member,refresh]);
 const stale=snapshot!==null&&Date.now()-snapshot.at>60000;
 const current=snapshot?.member===member&&(!stale||expanded)?snapshot:null;
 const records=(current?.page.records??[]).filter(r=>category==='all'||r.category===category);
 const series=seriesFor(records);const selected=members.find(m=>m.id===member);const timezone=state.home?.home.timezone??'Asia/Singapore';
 const choose=(id:string)=>{if(id!==member){setSnapshot(null);setExpanded(false);setMember(id);setCategory('all');}};
 const loadMore=async()=>{
  const current=latest.current;if(!current?.page.next_cursor||moreBusy)return;
  // Keep the selected history window stable instead of replacing it on a poll.
  expandedRef.current=true;setExpanded(true);
  setMoreBusy(true);setMoreError(false);const epoch=requestEpoch.current;
  try{const page=await api.getFamilyRecords(current.member,current.page.next_cursor);if(epoch!==requestEpoch.current)return;if(!Array.isArray(page.records))throw new Error('Invalid records response');
   setSnapshot(old=>old?.member===current.member?{...old,page:{records:[...old.page.records,...page.records.filter(r=>!old.page.records.some(p=>p.id===r.id))],next_cursor:page.next_cursor},at:Date.now()}:old);
  }catch{if(epoch===requestEpoch.current)setMoreError(true);}finally{if(epoch===requestEpoch.current)setMoreBusy(false);}
 };
 const sideCount=members.length+CATEGORIES.length;
 const sideDirection=(index:number,key:string)=>{if(key==='right')reading.current?.focus();if(key==='up'){if(index===0)focusTopNav('family');else controls.current[index-1]?.focus();}if(key==='down'){if(index+1<sideCount)controls.current[index+1]?.focus();else refreshButton.current?.focus();}};
 return <div className="page family-page"><TopNav onDown={()=>{const target=controls.current[0]??reading.current;target?.focus();}}/>
  <div className="page__heading"><h1 className="page__title">{selected?`${selected.name}的家庭记录`:'家庭记录'}</h1><span className="muted family-page__status" role="status">{status==='failed'?'暂时无法更新':status==='loading'?'正在读取…':expanded?'正在查看历史记录':'每 15 秒更新'}{stale?' · 记录已过期，请刷新':''}</span></div>
  <main className="page__body family-page__layout">
   <aside className="family-sidebar"><h2 className="page__section-title">家人</h2>
    {members.map((m,i)=><RemoteButton key={m.id} ref={el=>{controls.current[i]=el;}} className="family-sidebar__member" aria-pressed={member===m.id} onClick={()=>choose(m.id)} onDirection={key=>sideDirection(i,key)}><span className="family-avatar" aria-hidden="true">{m.name.slice(0,1)}</span>{m.name}</RemoteButton>)}
    <div className="family-sidebar__categories">{CATEGORIES.map((c,i)=><RemoteButton key={c.id} ref={el=>{controls.current[members.length+i]=el;}} className="family-category" aria-pressed={category===c.id} onClick={()=>setCategory(c.id)} onDirection={key=>sideDirection(members.length+i,key)}><svg viewBox="0 0 24 24" aria-hidden="true"><path d={c.icon}/></svg>{c.label}</RemoteButton>)}</div>
    <RemoteButton ref={refreshButton} className="btn" onClick={()=>{setExpanded(false);setRefresh(n=>n+1);}} onDirection={key=>{if(key==='up')controls.current[sideCount-1]?.focus();if(key==='right')reading.current?.focus();}}>刷新记录</RemoteButton>
   </aside>
   <div className="family-content" ref={reading} role="region" aria-label="家庭记录与趋势" tabIndex={0} onKeyDown={event=>onReadingKey(event,{left:()=>controls.current[0]?.focus(),top:()=>focusTopNav('family'),bottom:()=>more.current?.focus()})}>
    {status==='failed'&&current?<p className="family-message">连接暂不可用 · 显示最近读取的记录</p>:null}
    {!selected&&status==='ready'?<div className="family-empty"><h2 className="serif">从一条记录开始</h2><p>家庭成员尚未设置。接入后，可通过 OpenClaw 记录成长、健康与饮食。</p></div>:null}
    {selected&&!current&&status!=='loading'?<div className="family-empty"><h2 className="serif">暂时无法读取记录</h2><p>请检查连接后刷新。</p></div>:null}
    {current&&!records.length?<div className="family-empty"><h2 className="serif">{current.page.records.length?'当前记录中没有这一类内容':'还没有可展示的记录'}</h2><p>通过 OpenClaw 记录生活，允许在电视展示的内容会出现在这里。</p></div>:null}
    {series.length?<><div className="family-section-heading"><h2 className="page__section-title">成长与健康趋势</h2><span className="muted">根据已记录数据</span></div><div className="family-trends">{series.map(s=><FamilyTrend key={JSON.stringify([s.metric,s.unit])} series={s} timezone={timezone}/>)}</div></>:null}
    {records.length?<section className="family-history"><div className="family-section-heading"><h2 className="page__section-title">{category==='meal'?'饮食安排':'生活的每一个刻度'}</h2><span className="muted">已加载 {records.length} 条{current?.page.next_cursor?' · 尚有更早记录':''}</span></div>
     {records.map(r=><article className="family-record" key={r.id}><div className="family-record__date">{new Intl.DateTimeFormat('zh-CN',{year:'numeric',month:'long',day:'numeric',timeZone:timezone}).format(Date.parse(r.occurred_at))}<span>{CATEGORIES.find(c=>c.id===r.category)?.label}</span></div><div className="family-record__body"><h3 className="serif">{r.title}</h3>{r.measurements?.length?<p className="family-record__values">{r.measurements.map(m=>`${METRICS[m.metric]??m.metric} ${m.value} ${m.unit}`).join(' · ')}</p>:null}{r.notes?<p className="family-record__notes">{r.notes}</p>:null}<p className="family-record__sync muted"><span className={`dot dot--${r.exported_at?'ok':'warn'}`}/>{r.exported_at?'已同步到 NAS':'NAS 待同步'}</p></div></article>)}
    </section>:null}
    {moreError?<p className="family-message" role="alert">更早记录暂时无法读取，请重试。</p>:null}
    {current?.page.next_cursor?<RemoteButton ref={more} className="btn family-more" aria-disabled={moreBusy} onClick={()=>void loadMore()} onDirection={key=>{if(key==='up')reading.current?.focus();if(key==='left')controls.current[0]?.focus();}}>{moreBusy?'正在读取更早记录…':'更早记录'}</RemoteButton>:null}
   </div>
  </main>
 </div>;
}
