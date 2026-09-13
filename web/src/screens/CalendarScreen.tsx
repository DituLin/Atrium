import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import { flushSync } from 'react-dom';
import { useApp } from '../app/context';
import { calendarDate, monthCells, shiftMonth } from '../app/calendar';
import type { CalendarMonth } from '../app/calendar';
import { isTimeUnverified, serverNow } from '../core/clock';
import { Masthead, PrimaryNav, focusPrimaryNav } from '../ui/PrimaryNav';
import { RemoteButton } from '../ui/RemoteButton';
const WEEK = ['一', '二', '三', '四', '五', '六', '日'];
export function CalendarScreen() {
 const { state } = useApp();
 const [entryNow, setEntryNow] = useState(Date.now);
 const [browsed, setBrowsed] = useState<CalendarMonth | null>(null);
 const controls = useRef<(HTMLButtonElement | null)[]>([]);
 // House powers remote refresh; whichever authorized response is newest owns
 // the household zone, so retained Home/Overview metadata cannot shadow it.
 const timezone = [
  { zone: state.house.snapshot?.home.timezone, at: state.house.snapshot?.generated_at },
  { zone: state.home?.home.timezone, at: state.home?.server_time },
  { zone: state.overview.snapshot?.home.timezone, at: state.overview.snapshot?.generated_at },
 ].filter(source => source.zone && source.at && Number.isFinite(Date.parse(source.at)))
  .sort((a, b) => Date.parse(b.at!) - Date.parse(a.at!))[0]?.zone;
 const now = Math.max(state.nowMs, entryNow);
 const today = calendarDate(serverNow(state.clock, now), timezone);
 const month = browsed ?? today;
 const title = month ? `${month.year}年${month.month}月` : '日历';
 useLayoutEffect(() => {
  const foreground = () => { if (document.visibilityState !== 'hidden') flushSync(() => setEntryNow(Date.now())); };
  document.addEventListener('visibilitychange', foreground);
  return () => document.removeEventListener('visibilitychange', foreground);
 }, []);
 useEffect(() => {
  const origin = state.router.restoreFocus ? Array.from(document.querySelectorAll<HTMLElement>('[data-return-focus]')).find(element => element.dataset.returnFocus === state.router.restoreFocus) : null;
  (origin ?? controls.current[1])?.focus();
 }, [state.router.restoreFocus]);
 const cells = month ? monthCells(month.year, month.month) : [];
 function move(delta: number) {
  if (!month) return;
  const next = shiftMonth(month, delta);
  if (next.year >= 1 && next.year <= 9999) setBrowsed(next);
 }
 return <div className="screen screen--calendar">
  <Masthead />
  <header className="calendar__heading"><div><p className="calendar__eyebrow">日历</p><h1 className="photos__title" aria-live="polite">{title}</h1></div>
   <p>{today ? `今天 · ${today.month}月${today.day}日 星期${WEEK[(today.weekday + 6) % 7]}` : '正在取得家庭时区…'}<span>家庭时区 · {timezone ?? '待确认'}{isTimeUnverified(state.clock, now) ? ' · 时间未经核验' : ''}</span></p>
  </header>
  <main className="calendar__main">
   {month && today ? <table className="calendar__table" aria-label={title}>
    <thead><tr>{WEEK.map(day => <th scope="col" key={day}>周{day}</th>)}</tr></thead>
    <tbody>{Array.from({length:6},(_,row)=><tr key={row}>{cells.slice(row*7,row*7+7).map((day,column)=>{
     const current = day === today.day && month.year === today.year && month.month === today.month;
     return <td key={column} aria-current={current ? 'date' : undefined}><span className={current ? 'calendar__today' : undefined}>{day ?? ''}</span></td>;
    })}</tr>)}</tbody>
   </table> : <div className="calendar__waiting" role="status">取得时区后显示月历</div>}
   <div className="calendar__controls">{['上个月','回到本月','下个月'].map((label,index)=><RemoteButton key={label} className="button button--quiet" ref={element=>{controls.current[index]=element;}} onClick={()=>index===1?setBrowsed(null):move(index===0?-1:1)} onDirection={key=>{
    if(key==='left'||key==='right') controls.current[Math.max(0,Math.min(2,index+(key==='left'?-1:1)))]?.focus();
    if(key==='up'||key==='down') focusPrimaryNav('calendar');
   }}>{label}</RemoteButton>)}</div>
  </main>
  <PrimaryNav onUp={()=>controls.current[1]?.focus()} />
 </div>;
}
