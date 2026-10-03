import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { useEffect } from 'react';
import { expect, it, vi } from 'vitest';
import { AppProvider } from '../app/AppProvider';
import { useApp } from '../app/context';
import type { AppContextValue } from '../app/context';
import { CurrentScreen } from '../App';
import { saveLastAuthOkAt } from '../core/authExpiry';
import { houseFixture } from '../test/houseFixture';
import { overviewFixture } from '../test/overviewFixture';
let app: AppContextValue;
function Harness(){const value=useApp();useEffect(()=>{app=value;},[value]);return <CurrentScreen/>;}
const record=(id:string,value:number)=>({id,member_id:'abu',category:'growth',occurred_at:`2026-10-0${id}T00:00:00Z`,title:'身高记录',notes:'测试记录',measurements:[{metric:'height',value,unit:'cm'}],display_on_tv:true,created_at:'2026-10-03T00:00:00Z'});
async function setup(){
 saveLastAuthOkAt(Date.now());vi.stubGlobal('WebSocket',class{send(){}close(){}});
 const fetch=vi.fn(async(path:string)=>{
  if(path.includes('/photos'))return new Response(JSON.stringify({items:[],next_cursor:null}));
  if(path.includes('/family/members'))return new Response(JSON.stringify({members:[{id:'abu',name:'阿布'},{id:'parent',name:'家长'}]}));
  if(path.includes('/family/records'))return new Response(JSON.stringify({records:path.includes('parent')?[]:[record('1',68),record('2',70)],next_cursor:path.includes('parent')?undefined:'older'}));
  if(path.includes('/family/house'))return new Response(JSON.stringify(houseFixture()));
  if(path.includes('/family/overview'))return new Response(JSON.stringify(overviewFixture()));
  return new Response(JSON.stringify({schema_version:1,server_time:new Date().toISOString(),version:1,home:houseFixture().home,widgets:[]}));
 });vi.stubGlobal('fetch',fetch);render(<AppProvider><Harness/></AppProvider>);await waitFor(()=>expect(app).toBeDefined());
 act(()=>app.dispatch({type:'router.navigate',route:{name:'family'}}));await screen.findByRole('heading',{name:'阿布的家庭记录'});return fetch;
}
it('shows growth chart and NAS pending state, switches members without old data',async()=>{
 await setup();await screen.findByRole('img',{name:/身高趋势/});expect(screen.getAllByText(/NAS 待同步/).length).toBeGreaterThan(0);
 fireEvent.click(screen.getByRole('button',{name:'家长'}));await screen.findByText('还没有可展示的记录');expect(screen.queryByRole('img',{name:/身高趋势/})).toBeNull();
});
it('supports remote navigation, categories and paged history',async()=>{
 const fetch=await setup();await screen.findByRole('img',{name:/身高趋势/});
 const member=screen.getByRole('button',{name:'阿布'});member.focus();fireEvent.keyDown(member,{key:'ArrowRight'});expect(document.activeElement).toBe(screen.getByRole('region',{name:'家庭记录与趋势'}));
 fireEvent.click(screen.getByRole('button',{name:'饮食计划'}));expect(screen.queryByRole('img',{name:/身高趋势/})).toBeNull();expect(screen.getByText('当前记录中没有这一类内容')).toBeDefined();
 fireEvent.click(screen.getByRole('button',{name:'全部记录'}));fireEvent.click(screen.getByRole('button',{name:'更早记录'}));await waitFor(()=>expect(fetch.mock.calls.some(([p])=>p.includes('cursor=older'))).toBe(true));
});
it('clears private family data when authorization is revoked',async()=>{
 await setup();await screen.findByRole('img',{name:/身高趋势/});act(()=>app.dispatch({type:'app.needsPairing'}));expect(screen.queryByRole('img',{name:/身高趋势/})).toBeNull();
});
it('keeps paged history when a previously started poll finishes late',async()=>{
 const original=window.setInterval.bind(window);let poll:(()=>void)|undefined;
 const interval=vi.spyOn(window,'setInterval').mockImplementation(((handler:TimerHandler,delay?:number,...args:unknown[])=>{if(delay===15000&&typeof handler==='function')poll=handler as ()=>void;return original(handler,delay,...args);}) as typeof window.setInterval);
 try{
  const fetch=await setup();await screen.findByRole('img',{name:/身高趋势/});
  let finish!:(response:Response)=>void;
  fetch.mockImplementationOnce(()=>new Promise<Response>(resolve=>{finish=resolve;}));
  act(()=>poll?.());
  fetch.mockResolvedValueOnce(new Response(JSON.stringify({records:[{...record('1',65),id:'old',title:'历史记录'}]})));
  fireEvent.click(screen.getByRole('button',{name:'更早记录'}));await screen.findByText('历史记录');
  await act(async()=>finish(new Response(JSON.stringify({members:[{id:'abu',name:'阿布'}]}))));
  expect(screen.getByText('历史记录')).toBeDefined();expect(screen.getByText('正在查看历史记录')).toBeDefined();
 }finally{interval.mockRestore();}
});
