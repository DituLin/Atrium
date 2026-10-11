import {act,fireEvent,render,screen,waitFor} from '@testing-library/react';
import {useReducer} from 'react';
import {expect,it,vi} from 'vitest';
import {AppContext} from '../app/context';
import {appReducer,createInitialState} from '../app/state';
import type {AppAction} from '../app/state';
import type {ApiClient} from '../core/api';
import type {ArchiveItem,ArchivePage} from '../types/archives';
import {FamilyScreen} from './FamilyScreen';
const item=(id:string,domain:string):ArchiveItem=>({document_id:id,document_revision:2,domain,document_type:'record',ownership:'household',parse_status:'parsed',entity_ids:[],dates:['2026-10'],evidence:[{field_name:'title',excerpt:`合成${id}`,confirmed:true,locator:{},start:0,end:3}],metadata_only:false,truncated:false,review_required:false});
function setup(){
 const page:ArchivePage={items:['health','education','finance','property'].map(d=>item(d,d)),generation:'1:1',coverage:{complete:true},index_status:'ready'};
 const api={getArchives:vi.fn(async()=>({...page,items:[...page.items]})),getArchiveEntities:vi.fn(async()=>({items:[{id:'house',kind:'property',names:['测试屋'],aliases:[]}],coverage:{complete:true}})),analyzeArchives:vi.fn(async()=>({data_epoch:1,generation:1,groups:[{metric:'documents',value:'4'}],coverage:{complete:true},algorithm_version:'archive-analysis/5'})),getArchivePreview:vi.fn(async()=>({blob:new Blob(["png"],{type:"image/png"}),pages:2})),getArchiveDocument:vi.fn(async(id:string)=>({item:item(id,'health'),original_saved:true,nas_status:'pending',processing:'ready',preview:'structured',generation:'1:1',coverage:{complete:true}}))} as unknown as ApiClient;
 let send!:(a:AppAction)=>void;
 function Harness(){const [state,dispatch]=useReducer(appReducer,0,createInitialState);send=dispatch;return <AppContext.Provider value={{api,state,dispatch,house:{load:async()=>{},invalidate(){}},overview:{load:async()=>{},invalidate(){}},clientVersion:'test',goBack(){}}}><FamilyScreen/></AppContext.Provider>;}
 const view=render(<Harness/>);return {api,view,dispatch:(a:AppAction)=>act(()=>send(a))};
}
it('starts with the whole family and four domains without registered people',async()=>{
 const h=setup();await screen.findByText('合成property');expect(h.api.getArchives).toHaveBeenCalledWith({limit:10},expect.anything());expect(screen.getByRole('button',{name:'全家'}).getAttribute('aria-pressed')).toBe('true');expect(screen.getAllByText('2026年10月')).toHaveLength(4);
 fireEvent.click(screen.getByRole('button',{name:'房屋 · 测试屋'}));await waitFor(()=>expect(h.api.getArchives).toHaveBeenCalledWith(expect.objectContaining({entity_id:'house'}),expect.anything()));
 fireEvent.click(screen.getByRole('button',{name:'待归属'}));await waitFor(()=>expect(h.api.getArchives).toHaveBeenCalledWith(expect.objectContaining({ownership:'unassigned'}),expect.anything()));
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
