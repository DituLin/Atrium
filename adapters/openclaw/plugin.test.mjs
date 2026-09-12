import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync,writeFileSync,rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { registerPlugin, TOOL_FIELDS } from './index.js';

function fixture(t, overrides={}) {
 const dir=mkdtempSync(join(tmpdir(),'atrium-plugin-'));t.after(()=>rmSync(dir,{recursive:true,force:true}));
 const tools=Object.entries(TOOL_FIELDS).map(([name,fields])=>({name,description:'household',inputSchema:{type:'object',properties:Object.fromEntries(fields.map(f=>[f,{type:'string'}])),additionalProperties:false,required:[]}}));
 const toolsFile=join(dir,'tools.json');writeFileSync(toolsFile,JSON.stringify(tools),{mode:0o600});
 const hooks={},factories=[],requests=[];
 const host={async rpc(req){requests.push(req);if(req.method==='call')return {tool_result:{content:[{type:'text',text:'safe result'}]},report:{TextZH:'已显示'}};return req.method==='end'?{closed:true}:{};},close(){}};
 const api={pluginConfig:{agentId:'home',hostBinary:'/fake/host',hostArgs:[],toolsFile},on(name,fn){hooks[name]=fn;},registerTool(fn,opts){factories.push({fn,opts});},registerService(){}};
 const plugin=registerPlugin(api,{host,...overrides});t.after(()=>plugin.close());
 const ctx={agentId:'home',sessionId:'session'};
 return {api,hooks,factories,requests,toolsFile,tools,ctx,plugin};
}
test('exact optional tools, trusted hooks and deterministic report',async t=>{
 const f=fixture(t);assert.equal(f.factories.length,1);assert.equal(f.factories[0].opts.optional,true);
 assert.equal(f.factories[0].fn({agentId:'other'}),null);
 const tools=f.factories[0].fn(f.ctx);assert.equal(tools.length,10);
 await f.hooks.before_agent_start({runId:'run'},f.ctx);
 await f.hooks.before_tool_call({runId:'run',toolCallId:'call',toolName:'home_get_status'},f.ctx);
 const result=await tools.find(x=>x.name==='home_get_status').execute('call',{});
 assert.deepEqual(result.content,[{type:'text',text:'safe result'},{type:'text',text:'已显示'}]);
 assert.deepEqual(f.requests.map(x=>x.method),['start','call']);assert.match(f.requests[1].run_id,/^[0-9a-f]{64}$/);
});
test('execute fails closed without hooks and parameter identity cannot be injected',async t=>{
 const f=fixture(t);const tool=f.factories[0].fn(f.ctx).find(x=>x.name==='home_get_status');
 assert.equal((await tool.execute('missing',{})).isError,true);assert.equal(f.requests.length,0);
 await f.hooks.before_agent_start({runId:'run'},f.ctx);
 await f.hooks.before_tool_call({runId:'run',toolCallId:'call',toolName:tool.name},f.ctx);
 assert.equal((await tool.execute('call',{operation_id:'injected'})).isError,true);
 assert.equal(f.requests.length,1);
});
test('ambiguous run context and failed start cannot authorize tools',async t=>{
 const f=fixture(t,{host:{async rpc(){throw new Error('secret');},close(){}}});
 await f.hooks.before_agent_start({runId:'run'},f.ctx);
 const blocked=await f.hooks.before_tool_call({runId:'run',toolCallId:'call',toolName:'home_get_status'},f.ctx);
 assert.equal(blocked.block,true);
 const tool=f.factories[0].fn(f.ctx)[0];assert.equal((await tool.execute('call',{})).isError,true);
});
test('timeout cannot be revived by a late start response',async t=>{
 let resolveStart;const f=fixture(t,{runTimeoutMs:10,host:{rpc(req){return req.method==='start'?new Promise(resolve=>{resolveStart=resolve;}):Promise.resolve({});},close(){}}});
 const start=f.hooks.before_agent_start({runId:'run'},f.ctx);await new Promise(r=>setTimeout(r,30));resolveStart({});await start;
 assert.equal((await f.hooks.before_tool_call({runId:'run',toolCallId:'call',toolName:'home_get_status'},f.ctx)).block,true);
});
test('crash invalidates old run and only a new run can start',async t=>{
 const host={async rpc(req){return req.method==='end'?{closed:true}:{};},close(){}};const f=fixture(t,{host});
 await f.hooks.before_agent_start({runId:'old'},f.ctx);host.onCrash();
 await f.hooks.before_agent_start({runId:'old'},f.ctx);
 assert.equal((await f.hooks.before_tool_call({runId:'old',toolCallId:'call',toolName:'home_get_status'},f.ctx)).block,true);
 await f.hooks.before_agent_start({runId:'new'},f.ctx);
 assert.equal(await f.hooks.before_tool_call({runId:'new',toolCallId:'newcall',toolName:'home_get_status'},f.ctx),undefined);
});
test('agent end, mismatched event context, foreign session fail closed',async t=>{
 const f=fixture(t);await f.hooks.before_agent_start({runId:'run'},f.ctx);
 assert.equal((await f.hooks.before_tool_call({runId:'run',toolCallId:'call',toolName:'home_get_status'},{...f.ctx,runId:'different'})).block,true);
 await f.hooks.before_tool_call({runId:'run',toolCallId:'call',toolName:'home_get_status'},f.ctx);
 const other=f.factories[0].fn({...f.ctx,sessionId:'other'})[0];assert.equal((await other.execute('call',{})).isError,true);
 await f.hooks.agent_end({runId:'run'},f.ctx);
 const own=f.factories[0].fn(f.ctx)[0];assert.equal((await own.execute('call',{})).isError,true);
});
test('inventory rejects widened schemas and credential fields',t=>{
 const f=fixture(t);
 for(const mutate of [v=>v.pop(),v=>v[0].name='shell',v=>v[0].inputSchema.properties.operation_id={type:'string'},v=>v[0].inputSchema.properties.secret={type:'string'}]){
 const data=structuredClone(f.tools);mutate(data);writeFileSync(f.toolsFile,JSON.stringify(data));assert.throws(()=>registerPlugin(f.api,{host:{close(){}}}),/invalid_tools/);
 }
});
test('end before start tombstones the run and prevents delayed start',async t=>{
 const f=fixture(t);
 await f.hooks.agent_end({runId:'ended'},f.ctx);
 await f.hooks.before_agent_start({runId:'ended'},f.ctx);
 assert.deepEqual(f.requests.map(x=>x.method),['end']);
 assert.equal((await f.hooks.before_tool_call({runId:'ended',toolCallId:'call',toolName:'home_get_status'},f.ctx)).block,true);
});
test('tool call reference cannot be rebound across runs and late execute stays denied',async t=>{
 const f=fixture(t);const tool=f.factories[0].fn(f.ctx).find(x=>x.name==='home_get_status');
 await f.hooks.before_agent_start({runId:'old'},f.ctx);
 await f.hooks.before_tool_call({runId:'old',toolCallId:'shared-call',toolName:tool.name},f.ctx);
 await f.hooks.agent_end({runId:'old'},f.ctx);
 await f.hooks.before_agent_start({runId:'new'},f.ctx);
 assert.equal((await f.hooks.before_tool_call({runId:'new',toolCallId:'shared-call',toolName:tool.name},f.ctx)).block,true);
 assert.equal((await tool.execute('shared-call',{})).isError,true);
 assert.equal(f.requests.some(x=>x.method==='call'),false);
});
test('failed or unconfirmed early end disables all future runs',async t=>{
 for(const outcome of ['throw','false']){
 let closed=false;const requests=[];const host={async rpc(req){requests.push(req);if(outcome==='throw')throw new Error('private-secret');return {closed:false};},close(){closed=true;}};
 const f=fixture(t,{host});await f.hooks.agent_end({runId:'ended'},f.ctx);await f.hooks.before_agent_start({runId:'new'},f.ctx);
 assert.equal(closed,true);assert.deepEqual(requests.map(x=>x.method),['end']);assert.equal(f.factories[0].fn(f.ctx),null);
 }
});
test('run capacity closes plugin without evicting historical ownership',async t=>{
 let closed=false;const requests=[];const host={async rpc(req){requests.push(req);return {closed:true};},close(){closed=true;}};
 const f=fixture(t,{host,maxRuns:1});await f.hooks.before_agent_start({runId:'first'},f.ctx);await f.hooks.agent_end({runId:'first'},f.ctx);
 await f.hooks.before_agent_start({runId:'second'},f.ctx);assert.equal(closed,true);assert.equal(f.factories[0].fn(f.ctx),null);assert.deepEqual(requests.map(x=>x.method),['start','end']);
});
test('call capacity closes plugin and cannot recycle closed run references',async t=>{
 let closed=false;const host={async rpc(){return {};},close(){closed=true;}};const f=fixture(t,{host,maxCalls:1});
 await f.hooks.before_agent_start({runId:'run'},f.ctx);await f.hooks.before_tool_call({runId:'run',toolCallId:'first',toolName:'home_get_status'},f.ctx);
 assert.equal((await f.hooks.before_tool_call({runId:'run',toolCallId:'second',toolName:'home_get_status'},f.ctx)).block,true);
 assert.equal(closed,true);assert.equal(f.factories[0].fn(f.ctx),null);
});
test('unknown end at capacity still attempts a tombstone before closing host',async t=>{
 let closed=false;const requests=[];const host={async rpc(req){assert.equal(closed,false);requests.push(req);return {closed:true};},close(){closed=true;}};
 const f=fixture(t,{host,maxRuns:1});await f.hooks.before_agent_start({runId:'first'},f.ctx);await f.hooks.agent_end({runId:'unknown'},f.ctx);
 assert.deepEqual(requests.map(x=>x.method),['start','end']);assert.equal(closed,true);assert.equal(f.factories[0].fn(f.ctx),null);
});
test('idle host release waits for starting runs and confirmed end',async t=>{
 let releases=0,startCount=0,resolveSecond;
 const host={rpc(req){if(req.method==='start'&&++startCount===2)return new Promise(resolve=>{resolveSecond=resolve;});return Promise.resolve({closed:true});},releaseIdle(){releases++;},close(){}};
 const f=fixture(t,{host});await f.hooks.before_agent_start({runId:'first'},f.ctx);
 const starting=f.hooks.before_agent_start({runId:'second'},f.ctx);
 await f.hooks.agent_end({runId:'first'},f.ctx);assert.equal(releases,0);
 resolveSecond({});await starting;assert.equal(releases,0);
 await f.hooks.agent_end({runId:'second'},f.ctx);assert.equal(releases,1);
 await f.hooks.before_agent_start({runId:'third'},f.ctx);assert.equal(releases,1);
});
test('identity diagnostics expose only stage hashes and presence flags',async t=>{
 const f=fixture(t);const logs=[];f.api.logger={info(line){logs.push(line);}};f.ctx.sessionId='private-session-secret';f.ctx.sessionKey='private-key-secret';
 const tools=f.factories[0].fn(f.ctx);await f.hooks.before_agent_start({runId:'private-run-secret',prompt:'private-prompt-secret'},f.ctx);
 await f.hooks.before_tool_call({runId:'private-run-secret',toolCallId:'private-call-secret',toolName:'home_get_status'},f.ctx);
 await tools.find(x=>x.name==='home_get_status').execute('private-call-secret',{secret:'private-argument-secret'});
 const output=logs.join('\n');assert.equal(output.includes('private-'),false);assert.equal(output.includes('execute'),true);assert.equal(output.includes('mapping_found'),true);
 for(const line of logs){const item=JSON.parse(line.replace('atrium_identity ',''));for(const key of ['session_hash','run_hash','call_hash'])assert.match(item[key],/^([0-9a-f]{64})?$/);}
});
test('separate OpenClaw registries share trusted controller and old stop cannot close new registry',async t=>{
 const sharedRegistry=new Map();const f=fixture(t,{sharedRegistry});const hooks={},factories=[];
 const secondApi={...f.api,on(n,fn){hooks[n]=fn;},registerTool(fn,opts){factories.push({fn,opts});}};
 const second=registerPlugin(secondApi,{sharedRegistry});t.after(()=>second.close());
 await f.hooks.before_agent_start({runId:'run'},f.ctx);await f.hooks.before_tool_call({runId:'run',toolCallId:'call',toolName:'home_get_status'},f.ctx);
 const tool=factories[0].fn(f.ctx).find(x=>x.name==='home_get_status');
 assert.equal((await tool.execute('call',{})).isError,undefined);
 f.plugin.close();assert.equal(await f.hooks.before_tool_call({runId:'run',toolCallId:'call',toolName:'home_get_status'},f.ctx),undefined);assert.equal((await tool.execute('call',{})).isError,undefined);
 await hooks.agent_end({runId:'run'},f.ctx);assert.equal((await tool.execute('call',{})).isError,true);
});
test('different trusted ledger configuration cannot reuse controller mappings',async t=>{
 const sharedRegistry=new Map();const f=fixture(t,{sharedRegistry});const factories=[];
 const api={...f.api,pluginConfig:{...f.api.pluginConfig,hostArgs:['--ledger','/different/ledger']},on(){},registerTool(fn){factories.push(fn);}};
 const second=registerPlugin(api,{sharedRegistry,host:{async rpc(){return {};},close(){}}});t.after(()=>second.close());
 await f.hooks.before_agent_start({runId:'run'},f.ctx);await f.hooks.before_tool_call({runId:'run',toolCallId:'call',toolName:'home_get_status'},f.ctx);
 assert.equal((await factories[0](f.ctx).find(x=>x.name==='home_get_status').execute('call',{})).isError,true);
});
test('all registrations revalidate config and stops are idempotent',async t=>{
 const sharedRegistry=new Map();const f=fixture(t,{sharedRegistry});
 assert.throws(()=>registerPlugin({...f.api,pluginConfig:{...f.api.pluginConfig,unexpected:true}},{sharedRegistry}),/invalid_atrium_config/);
 const hooks={},factories=[];const second=registerPlugin({...f.api,on(n,fn){hooks[n]=fn;},registerTool(fn){factories.push(fn);}},{sharedRegistry});t.after(()=>second.close());
 await hooks.before_agent_start({runId:'run'},f.ctx);await hooks.before_tool_call({runId:'run',toolCallId:'call',toolName:'home_get_status'},f.ctx);
 const oldTool=f.factories[0].fn(f.ctx).find(x=>x.name==='home_get_status');f.plugin.close();f.plugin.close();
 assert.equal((await oldTool.execute('call',{})).isError,true);assert.equal((await factories[0](f.ctx).find(x=>x.name==='home_get_status').execute('call',{})).isError,undefined);
 second.close();second.close();assert.equal(sharedRegistry.size,0);
});
test('independent module copies share production global controller with real host',async t=>{
 const f=fixture(t);const source=`process.stdin.setEncoding('utf8');let b='';process.stdin.on('data',chunk=>{b+=chunk;let n;while((n=b.indexOf('\\n'))>=0){const x=JSON.parse(b.slice(0,n));b=b.slice(n+1);process.stdout.write(JSON.stringify({id:x.id,result:x.method==='call'?{tool_result:{content:[{type:'text',text:'shared controller'}]}}:{closed:true}})+'\\n');}});`;
 const config={...f.api.pluginConfig,hostBinary:process.execPath,hostArgs:['-e',source]};const hooks={},factories=[];
 const a=await import('./index.js?registry-a');const b=await import('./index.js?registry-b');
 const one=a.registerPlugin({...f.api,pluginConfig:config,on(n,fn){hooks[n]=fn;},registerTool(){}});
 const two=b.registerPlugin({...f.api,pluginConfig:config,on(){},registerTool(fn){factories.push(fn);}});t.after(()=>{one.close();two.close();});
 await hooks.before_agent_start({runId:'run'},f.ctx);await hooks.before_tool_call({runId:'run',toolCallId:'call',toolName:'home_get_status'},f.ctx);
 const result=await factories[0](f.ctx).find(x=>x.name==='home_get_status').execute('call',{});assert.deepEqual(result.content,[{type:'text',text:'shared controller'}]);
 await hooks.agent_end({runId:'run'},f.ctx);
});
