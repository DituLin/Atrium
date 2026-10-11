import {act,render,waitFor} from '@testing-library/react';
import {StrictMode,useReducer} from 'react';
import {expect,it,vi} from 'vitest';
import type {ApiClient} from '../core/api';
import type {WsClientOptions} from '../core/ws';
import type * as WsModule from '../core/ws';
import {FamilyScreen} from '../screens/FamilyScreen';
import type {CommandAckPayload,ServerMessage} from '../types/ws';
import {AppContext} from './context';
import {appReducer,createInitialState} from './state';
import type {AppAction,AppState} from './state';
import {useConnection} from './useConnection';
const socket=vi.hoisted(()=>({options:null as WsClientOptions|null,acks:[] as CommandAckPayload[]}));
vi.mock('../core/ws',async(original)=>({...await original<typeof WsModule>(),createWsClient:(options:WsClientOptions)=>{socket.options=options;return {start(){},stop(){},sendState(){},sendAck(ack:CommandAckPayload){socket.acks.push(ack);}};}}));
const page={items:[],generation:'1:1',coverage:{complete:true},index_status:'ready'};
const analysis={data_epoch:1,generation:1,groups:[],coverage:{complete:true},projection:'public'};
function harness(strict=false){
 socket.acks=[];let current!:AppState;let send!:(a:AppAction)=>void;
 const api={getArchives:vi.fn(async()=>({...page})),getArchiveEntities:vi.fn(async()=>({items:[],coverage:{complete:true}})),analyzeArchives:vi.fn(async()=>analysis),getArchiveView:vi.fn(async(id:string)=>({archive_view_id:id,page:{...page},analysis}))} as unknown as ApiClient;
 const house={load:async()=>{},invalidate(){}};
 function Harness(){const [state,dispatch]=useReducer(appReducer,0,createInitialState);current=state;send=dispatch;useConnection({house,api,state,dispatch,enabled:true,clientVersion:'test'});return <AppContext.Provider value={{api,state,dispatch,house,overview:house,clientVersion:'test',goBack(){}}}>{state.router.route.name==='family'?<FamilyScreen/>:<p>首页</p>}</AppContext.Provider>;}
 const view=render(strict?<StrictMode><Harness/></StrictMode>:<Harness/>);
 return {api,view,state:()=>current,dispatch:(a:AppAction)=>act(()=>send(a)),command:(n:number)=>act(()=>socket.options!.onMessage({schema_version:1,type:'screen.command',id:`event${n}`,sent_at:'2026-10-11T00:00:00Z',payload:{command_id:`cmd${n}`,sequence:n,kind:'archive',payload:{archive_view_id:'view'},issued_at:'2026-10-11T00:00:00Z',expires_at:'2026-10-11T00:00:10Z'}} as ServerMessage))};
}
it('holds accepted archive command until its own current public result has rendered',async()=>{
 const h=harness();let finish!:(v:unknown)=>void;vi.mocked(h.api.getArchiveView).mockImplementation(()=>new Promise(resolve=>{finish=resolve as typeof finish;}));h.command(1);expect(socket.acks).toHaveLength(0);expect(h.state().router.appliedSequence).toBe(0);
 vi.mocked(h.api.getArchiveView).mockResolvedValue({archive_view_id:'view',page,analysis} as never);await act(async()=>finish({archive_view_id:'view',page,analysis}));await waitFor(()=>expect(socket.acks[0]?.status).toBe('applied'));expect(socket.acks[0]?.resource_id).toBe('view');expect(h.state().router.appliedSequence).toBe(1);
});
it('requires a fresh same-view result and ignores old command completion',async()=>{
 const h=harness();h.command(1);await waitFor(()=>expect(socket.acks[0]?.status).toBe('applied'));
 vi.mocked(h.api.getArchiveView).mockImplementation(()=>new Promise(()=>{}));h.command(2);expect(socket.acks).toHaveLength(1);h.dispatch({type:'archives.rendered',commandId:'cmd1',id:'view',status:'ready'});expect(socket.acks).toHaveLength(1);
 h.dispatch({type:'router.navigate',route:{name:'dashboard'}});await waitFor(()=>expect(socket.acks[1]?.status).toBe('failed'));
});
it('fails an expired or revoked view rather than acknowledging navigation',async()=>{
 const h=harness();vi.mocked(h.api.getArchiveView).mockRejectedValue(new Error('expired'));h.command(1);await waitFor(()=>expect(socket.acks[0]?.status).toBe('failed'));expect(socket.acks[0]?.error_code).toBe('archive_unavailable');expect(h.state().router.appliedSequence).toBe(0);
});
it('loads after StrictMode setup cleanup replay',async()=>{const h=harness(true);h.command(1);await waitFor(()=>expect(socket.acks[0]?.status).toBe('applied'));});
