import {act,fireEvent,render,screen,waitFor} from '@testing-library/react';
import {useReducer} from 'react';
import {afterEach,expect,it,vi} from 'vitest';
afterEach(() => vi.useRealTimers());
import {AppContext} from '../app/context';
import {appReducer,createInitialState} from '../app/state';
import type {AppAction,AppState} from '../app/state';
import {ApiError, type ApiClient} from '../core/api';
import type {ArchiveItem,ArchivePage} from '../types/archives';
import {FamilyScreen} from './FamilyScreen';
const item=(id:string,domain:string):ArchiveItem=>({document_id:id,document_revision:2,domain,document_type:'record',ownership:'household',parse_status:'parsed',entity_ids:[],dates:['2026-10'],evidence:[{field_name:'title',excerpt:`合成${id}`,confirmed:true,locator:{},start:0,end:3}],metadata_only:false,truncated:false,review_required:false});
function setup(){
 const page:ArchivePage={items:['health','education','finance','property'].map(d=>item(d,d)),generation:'1:1',coverage:{complete:true},index_status:'ready'};
 const api={getArchives:vi.fn(async()=>({...page,items:[...page.items]})),getArchiveOverview:vi.fn(async()=>({generation:'1:1',total:128,confirmed:124,pending:4,domains:[{domain:'health',count:56},{domain:'finance',count:34},{domain:'education',count:22},{domain:'property',count:16}],entities:[{id:'house',count:14}],ownership:{household:19,unassigned:5,waiting:3},coverage:{complete:true}})),getArchiveEntities:vi.fn(async()=>({items:[{id:'house',kind:'property',names:['测试屋'],aliases:[]}],coverage:{complete:true}})),analyzeArchives:vi.fn(async()=>({data_epoch:1,generation:1,groups:[{metric:'documents',value:'4'}],coverage:{complete:true},algorithm_version:'archive-analysis/5'})),getArchivePreview:vi.fn(async()=>({blob:new Blob(["png"],{type:"image/png"}),pages:2})),getArchiveDocument:vi.fn(async(id:string)=>({item:item(id,'health'),original_saved:true,nas_status:'pending',processing:'ready',preview:'structured',generation:'1:1',coverage:{complete:true}}))} as unknown as ApiClient;
 let send!:(a:AppAction)=>void;let latest!:AppState;
 function Harness(){const [state,dispatch]=useReducer(appReducer,0,createInitialState);send=dispatch;latest=state;return <AppContext.Provider value={{api,state,dispatch,house:{load:async()=>{},invalidate(){}},overview:{load:async()=>{},invalidate(){}},clientVersion:'test',goBack(){}}}><FamilyScreen/></AppContext.Provider>;}
 const view=render(<Harness/>);return {api,view,state:()=>latest,dispatch:(a:AppAction)=>act(()=>send(a))};
}
it('starts with the whole family and four domains without registered people',async()=>{
 const h=setup();await screen.findByText('合成property');expect(h.api.getArchives).toHaveBeenCalledWith({limit:10},expect.anything());expect(screen.getByRole('button',{name:'全家'}).getAttribute('aria-pressed')).toBe('true');expect(screen.getAllByText('2026年10月')).toHaveLength(4);
 fireEvent.click(screen.getByRole('button',{name:'房屋 · 测试屋'}));await waitFor(()=>expect(h.api.getArchives).toHaveBeenCalledWith(expect.objectContaining({entity_id:'house'}),expect.anything()));await waitFor(()=>expect(h.view.container.querySelector('.family-page__scope')?.textContent).toBe('测试屋'));expect(screen.getByRole('heading',{name:'奶量 · 测试屋'})).toBeTruthy();
 fireEvent.click(screen.getByRole('button',{name:'待归属'}));await waitFor(()=>expect(h.api.getArchives).toHaveBeenCalledWith(expect.objectContaining({ownership:'unassigned'}),expect.anything()));expect(h.view.container.querySelector('.family-page__scope')?.textContent).toBe('待归属');
});
it('returns to the same card with remote Back and does not treat reading as card activation',async()=>{
 setup();const card=await screen.findByRole('button',{name:/合成health/});card.focus();fireEvent.keyDown(card,{key:'Enter'});await screen.findByRole('button',{name:'返回档案'});await screen.findByText('NAS 待同步',{exact:false});fireEvent.keyDown(screen.getByRole('button',{name:'返回档案'}),{key:'Escape'});await waitFor(()=>expect(document.activeElement?.textContent).toContain('合成health'));
});
it('clears details synchronously on archive invalidation and ignores old requests',async()=>{
 const h=setup();fireEvent.click(await screen.findByRole('button',{name:/合成health/}));await screen.findByText('结构化内容与来源');vi.mocked(h.api.getArchives).mockImplementation(()=>new Promise(()=>{}));h.dispatch({type:'archives.invalidate'});expect(screen.queryByText('结构化内容与来源')).toBeNull();expect(screen.queryByText('合成health')).toBeNull();
});

it('keeps remote focus usable at both PDF page boundaries and revokes preview URLs',async()=>{
 const create=vi.fn(()=> 'blob:synthetic');const revoke=vi.fn();
 Object.defineProperty(URL,'createObjectURL',{configurable:true,value:create});Object.defineProperty(URL,'revokeObjectURL',{configurable:true,value:revoke});
 const h=setup();vi.mocked(h.api.getArchiveDocument).mockResolvedValue({item:item('health','health'),original_saved:true,nas_status:'pending',processing:'ready',preview:'pages',generation:'1:1',coverage:{complete:true}});
 fireEvent.click(await screen.findByRole('button',{name:/合成health/}));await screen.findByRole('img',{name:'档案原件第 1 页'});
 const next=screen.getByRole('button',{name:'下一页'});next.focus();fireEvent.keyDown(next,{key:'Enter'});
 await screen.findByRole('img',{name:'档案原件第 2 页'});expect(document.activeElement?.textContent).toBe('上一页');
 fireEvent.keyDown(document.activeElement!,{key:'Enter'});await screen.findByRole('img',{name:'档案原件第 1 页'});expect(document.activeElement?.textContent).toBe('下一页');
 vi.mocked(h.api.getArchives).mockImplementation(()=>new Promise(()=>{}));h.dispatch({type:'archives.invalidate'});expect(screen.queryByRole('img')).toBeNull();expect(revoke).toHaveBeenCalled();
});

const queryResult=(id:string)=>({archive_view_id:id,page:{items:[item(id,'finance')],generation:'1:1',coverage:{complete:true},index_status:'ready'},analysis:{data_epoch:1,generation:1,groups:[],coverage:{complete:true},algorithm_version:'archive-analysis/5'}});
function showQuery(h:ReturnType<typeof setup>,id:string){h.dispatch({type:'archives.open',commandId:id});h.dispatch({type:'router.navigate',route:{name:'family',archiveViewId:id,commandId:id}});}
it('shows the requested query cards before acknowledging after leaving a detail',async()=>{
 const h=setup();fireEvent.click(await screen.findByRole('button',{name:/合成health/}));await screen.findByText('结构化内容与来源');
 h.api.getArchiveView=vi.fn(async(id:string)=>queryResult(id)) as never;showQuery(h,'new-query');
 await waitFor(()=>expect(h.state().archiveRender.status).toBe('ready'));
 expect(screen.queryByText('该档案暂不可读取。')).toBeNull();expect(screen.getByRole('button',{name:/合成new-query/})).toBeTruthy();expect(screen.getByRole('heading',{name:'投屏查询结果'})).toBeTruthy();expect(screen.getByRole('button',{name:'全家'}).getAttribute('aria-pressed')).toBe('false');expect(screen.getByRole('button',{name:'全部领域'}).getAttribute('aria-pressed')).toBe('false');
});
it('replaces an earlier query only with the new query DOM and ignores reset responses',async()=>{
 const h=setup();h.api.getArchiveView=vi.fn(async(id:string)=>queryResult(id)) as never;showQuery(h,'first');await screen.findByRole('button',{name:/合成first/});
 let finish!:(result:unknown)=>void;vi.mocked(h.api.getArchiveView).mockImplementation(()=>new Promise(resolve=>{finish=resolve as typeof finish;}));showQuery(h,'second');
 expect(screen.queryByRole('button',{name:/合成first/})).toBeNull();expect(h.state().archiveRender.status).toBe('loading');
 vi.mocked(h.api.getArchiveView).mockResolvedValue(queryResult('second') as never);await act(async()=>finish(queryResult('second')));await waitFor(()=>expect(h.state().archiveRender.status).toBe('ready'));expect(screen.getByRole('button',{name:/合成second/})).toBeTruthy();
 vi.mocked(h.api.getArchiveView).mockImplementation(()=>new Promise(resolve=>{finish=resolve as typeof finish;}));showQuery(h,'discarded');
 fireEvent.click(screen.getByRole('button',{name:'财务'}));await screen.findByRole('button',{name:/合成finance/});
 await act(async()=>finish(queryResult('discarded')));expect(screen.queryByRole('button',{name:/合成discarded/})).toBeNull();expect(h.state().archiveRender.status).not.toBe('ready');expect(h.state().router.route).toEqual({name:'family'});
});

it('shows verified cards with an explicit analysis limit instead of hiding the archive page',async()=>{
 const h=setup();vi.mocked(h.api.analyzeArchives).mockRejectedValue(new ApiError(409,'conflict','bounded',null,{reason:'analysis_response_limit'}));
 await screen.findByRole('button',{name:/合成health/});expect(screen.getByText('奶量统计暂不可用')).toBeTruthy();expect(screen.getByRole('button',{name:'财务金额'}).textContent).toContain('—');expect(screen.queryByText('等待重新核验')).toBeNull();expect(screen.queryByText('200')).toBeNull();
});

it('keeps the milk chart and day/week/month controls visible when only one feed is recorded',async()=>{
 // Keep the observation inside the selected date range regardless of run date.
 vi.useFakeTimers({toFake:['Date']});
 vi.setSystemTime(new Date('2026-10-11T12:00:00Z'));
 const h=setup();
 vi.mocked(h.api.analyzeArchives).mockImplementation(async q=>({snapshot_id:'milk',data_epoch:1,generation:1,projection:'public',algorithm_version:'archive-analysis/6',coverage:{complete:true,included:1,excluded:0,reasons:[]},groups:q.metric==='formula'?[{metric:'formula',unit:'ml',date:'2026-10-11T09:00:00Z',value:'60'}]:[{metric:'documents',value:'4'}]}));
 await screen.findByRole('heading',{name:'奶量 · 全家'});
 expect(screen.getByRole('button',{name:'日'}).getAttribute('aria-pressed')).toBe('true');
 expect(screen.getByRole('button',{name:'周'})).toBeTruthy();
 expect(screen.getByRole('button',{name:'月'})).toBeTruthy();
 expect(screen.getByRole('img',{name:/奶量 · 全家，每次记录/})).toBeTruthy();
 const day=screen.getByRole('button',{name:'日'});day.focus();fireEvent.keyDown(day,{key:'ArrowRight'});
 expect(document.activeElement).toBe(screen.getByRole('button',{name:'周'}));
 fireEvent.keyDown(document.activeElement!,{key:'Enter'});
 await waitFor(()=>expect(h.api.analyzeArchives).toHaveBeenCalledWith(expect.objectContaining({operation:'sum',domain:'health',date_from:expect.any(String),date_to:expect.any(String)}),expect.anything()));
 expect(screen.getByRole('button',{name:'周'}).getAttribute('aria-pressed')).toBe('true');
 expect(document.activeElement).toBe(screen.getByRole('button',{name:'周'}));
 fireEvent.keyDown(document.activeElement!,{key:'ArrowRight'});
 expect(document.activeElement).toBe(screen.getByRole('button',{name:'月'}));
});


it('shows compact person growth cards with measured deltas and hides them on revocation',async()=>{
 const h=setup();
 vi.mocked(h.api.getArchiveEntities).mockResolvedValue({items:[{id:'child',kind:'person',names:['合成宝宝'],aliases:[]}],coverage:{complete:true}});
 vi.mocked(h.api.analyzeArchives).mockImplementation(async q=>({snapshot_id:'growth',data_epoch:1,generation:1,projection:'public',algorithm_version:'test',coverage:{complete:true,included:2,excluded:0,reasons:[]},groups:q.metric==='height'||q.metric==='体重'?[{metric:q.metric,unit:q.metric==='height'?'cm':'kg',date:'2026-06-13',value:q.metric==='height'?'60.50':'6.250'},{metric:q.metric,unit:q.metric==='height'?'cm':'kg',date:'2026-10-13',value:q.metric==='height'?'70.25':'8.100'}]:[]}));
 fireEvent.click(await screen.findByRole('button',{name:'合成宝宝'}));
 await screen.findByRole('img',{name:/身高 · 合成宝宝/});
 expect(screen.getByRole('img',{name:/体重 · 合成宝宝/})).toBeTruthy();
 expect(screen.getByRole('button',{name:'身高 · 合成宝宝'}).textContent).toContain('+9.75 cm');expect(screen.getByRole('button',{name:'体重 · 合成宝宝'}).textContent).toContain('+1.85 kg');
 const day=screen.getByRole('button',{name:'日'});day.focus();fireEvent.keyDown(day,{key:'ArrowDown'});
 expect(document.activeElement).toBe(screen.getByRole('button',{name:'身高'}));
 fireEvent.keyDown(document.activeElement!,{key:'ArrowRight'});expect(document.activeElement).toBe(screen.getByRole('button',{name:'体重'}));
 fireEvent.keyDown(document.activeElement!,{key:'ArrowDown'});expect(document.activeElement).toBe(screen.getByRole('region',{name:'家庭档案与分析'}));
 expect(h.api.analyzeArchives).toHaveBeenCalledWith(expect.objectContaining({metric:'height',entity_id:'child'}),expect.anything());
 vi.mocked(h.api.getArchives).mockImplementation(()=>new Promise(()=>{}));h.dispatch({type:'archives.invalidate'});
 expect(screen.queryByRole('img',{name:/身高 · 合成宝宝/})).toBeNull();expect(screen.queryByText('70.25')).toBeNull();
});

it('keeps the six design slots and whole-family counts independent of the visible archive page',async()=>{
 const h=setup();await screen.findByText('合成property');
 const dashboard=screen.getByRole('region',{name:'公开档案分析'});
 expect(dashboard.querySelectorAll('.archive-dashboard__grid > *')).toHaveLength(6);
 expect(dashboard.querySelector('.archive-dashboard__grid')?.firstElementChild?.className).toContain('archive-dashboard__milk');
 expect(screen.getByRole('button',{name:'档案构成 · 全家公开范围'}).textContent).toContain('128');
 expect(screen.getByRole('button',{name:'全家'}).textContent).toContain('128');
 expect(screen.getByRole('button',{name:'房屋 · 测试屋'}).textContent).toContain('14');
 fireEvent.click(screen.getByRole('button',{name:'财务'}));await waitFor(()=>expect(h.api.getArchives).toHaveBeenCalledWith(expect.objectContaining({domain:'finance'}),expect.anything()));
 await waitFor(()=>expect(screen.getByRole('button',{name:'档案构成 · 全家公开范围'}).textContent).toContain('128'));
 expect(screen.getAllByRole('button',{name:/合成(?:health|education|finance|property)/})).toHaveLength(4);
});

it('reaches the domain pills with the remote and enters analysis controls without applying a focused domain', async () => {
 const h=setup();await screen.findByText('合成property');
 const whole=screen.getByRole('button',{name:'全家'});whole.focus();
 fireEvent.keyDown(whole,{key:'ArrowRight'});
 expect(document.activeElement).toBe(screen.getByRole('button',{name:'全部领域'}));
 fireEvent.keyDown(document.activeElement!,{key:'ArrowRight'});
 expect(document.activeElement).toBe(screen.getByRole('button',{name:'健康'}));
 expect(h.api.getArchives).not.toHaveBeenCalledWith(expect.objectContaining({domain:'health'}),expect.anything());
 fireEvent.keyDown(document.activeElement!,{key:'ArrowDown'});
 expect(document.activeElement).toBe(screen.getByRole('button',{name:'日'}));
 fireEvent.keyDown(document.activeElement!,{key:'ArrowUp'});
 expect(document.activeElement).toBe(screen.getByRole('button',{name:'全部领域'}));
});
