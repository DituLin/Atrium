import test from 'node:test';
import assert from 'node:assert/strict';
import { EventEmitter } from 'node:events';
import { PassThrough, Writable } from 'node:stream';
import { Host } from './host.js';
function fixture(t,timeoutMs=100, maxPending=64){
 const requests=[];let child;let spawns=0;
 const host=new Host({hostBinary:'/safe/host',hostArgs:['--stdio']},{timeoutMs,maxPending,spawnHost(binary,args,options){
 assert.equal(options.shell,false);spawns++;child=new EventEmitter();child.stdout=new PassThrough();child.kill=()=>{};
 child.stdin=new Writable({write(chunk,encoding,cb){requests.push(JSON.parse(chunk));cb();}});return child;
 }});t.after(()=>host.close());return {host,requests,get child(){return child;},get spawns(){return spawns;}};
}
test('JSONL response and abort sends run cancel without action retry',async t=>{
 const f=fixture(t);const controller=new AbortController();const pending=f.host.rpc({method:'call',run_id:'run',call_id:'call'},controller.signal);
 controller.abort();await assert.rejects(pending,/cancelled/);assert.deepEqual(f.requests.map(x=>x.method),['call','cancel']);
 const next=f.host.rpc({method:'tools'});const id=f.requests.at(-1).id;f.child.stdout.write(JSON.stringify({id,result:{text:'中文'}})+'\n');assert.deepEqual(await next,{text:'中文'});assert.equal(f.spawns,1);
});
test('timeout sends cancel and clears pending RPC',async t=>{
 const f=fixture(t,10);await assert.rejects(f.host.rpc({method:'call',run_id:'run'}),/cancelled/);assert.equal(f.host.pending.size,1);f.child.stdout.write(JSON.stringify({id:f.requests.at(-1).id,result:{closed:true}})+'\n');assert.equal(f.host.pending.size,0);assert.deepEqual(f.requests.map(x=>x.method),['call','cancel']);
});
test('crash rejects in flight requests, malformed protocol is never surfaced',async t=>{
 const f=fixture(t);let crashes=0;f.host.onCrash=()=>{crashes++;};const pending=f.host.rpc({method:'start',run_id:'run'});
 f.child.stdout.write('private-secret\n');await assert.rejects(pending,/host_unavailable/);assert.equal(crashes,1);
 const next=f.host.rpc({method:'start',run_id:'new'});assert.equal(f.spawns,2);f.child.stdout.write(JSON.stringify({id:f.requests.at(-1).id,result:{}})+'\n');await next;
});
test('split UTF8 preserved and errors retain only canonical recovery IDs',async t=>{
 const f=fixture(t);const call=f.host.rpc({method:'tools'});const bytes=Buffer.from(JSON.stringify({id:f.requests.at(-1).id,result:{text:'中文'}})+'\n');const offset=bytes.indexOf(Buffer.from('中'))+1;
 f.child.stdout.write(bytes.subarray(0,offset));f.child.stdout.write(bytes.subarray(offset));assert.deepEqual(await call,{text:'中文'});
 const pending=f.host.rpc({method:'call',run_id:'run'});f.child.stdout.write(JSON.stringify({id:f.requests.at(-1).id,error:{code:'private-secret',message:'private-secret',operation_id:'01K4D6H0000000000000000001',command_id:'private-secret'}})+'\n');
 await assert.rejects(pending,error=>error.code==='host_error'&&error.operation_id==='01K4D6H0000000000000000001'&&error.command_id===undefined&&!error.message.includes('secret'));
});
test('real child process JSONL roundtrip and inherited lifecycle',async t=>{
 const source=`process.stdin.setEncoding('utf8');let b='';process.stdin.on('data',chunk=>{b+=chunk;let n;while((n=b.indexOf('\\n'))>=0){const x=JSON.parse(b.slice(0,n));b=b.slice(n+1);process.stdout.write(JSON.stringify({id:x.id,result:{method:x.method,text:'中文'}})+'\\n');}});`;
 const host=new Host({hostBinary:process.execPath,hostArgs:['-e',source]});t.after(()=>host.close());
 assert.deepEqual(await host.rpc({method:'start',run_id:'run'}),{method:'start',text:'中文'});
 const first=host.child.pid;
 assert.deepEqual(await host.rpc({method:'tools'}),{method:'tools',text:'中文'});assert.equal(host.child.pid,first);
 const exited=new Promise(resolve=>host.child.once('exit',resolve));host.close();await exited;
});
test('already aborted call closes the active run without dispatching the action',async t=>{
 const f=fixture(t);const start=f.host.rpc({method:'start',run_id:'run'});f.child.stdout.write(JSON.stringify({id:f.requests[0].id,result:{}})+'\n');await start;
 const signal=AbortSignal.abort();await assert.rejects(f.host.rpc({method:'call',run_id:'run'},signal),/cancelled/);
 assert.deepEqual(f.requests.map(x=>x.method),['start','cancel']);
});
test('pending capacity permanently closes host and rejects all inflight requests',async t=>{
 const f=fixture(t,100,1);
 const first=f.host.rpc({method:'start',run_id:'one'});const firstFailure=assert.rejects(first,/host_closed/);
 await assert.rejects(f.host.rpc({method:'start',run_id:'two'}),/capacity_exceeded/);await firstFailure;
 assert.equal(f.host.pending.size,0);assert.equal(f.host.closed,true);assert.equal(f.requests.length,1);
 await assert.rejects(f.host.rpc({method:'tools'}),/host_unavailable/);
});
test('idle release exits real child and permits a later run to spawn',async t=>{
 const source=`process.stdin.setEncoding('utf8');let b='';process.stdin.on('data',chunk=>{b+=chunk;let n;while((n=b.indexOf('\\n'))>=0){const x=JSON.parse(b.slice(0,n));b=b.slice(n+1);process.stdout.write(JSON.stringify({id:x.id,result:{closed:true}})+'\\n');}});`;
 const host=new Host({hostBinary:process.execPath,hostArgs:['-e',source]});t.after(()=>host.close());
 await host.rpc({method:'start',run_id:'first'});const old=host.child;await host.rpc({method:'end',run_id:'first'});
 const exited=new Promise(resolve=>old.once('exit',resolve));assert.equal(host.releaseIdle(),true);await exited;assert.equal(host.child,null);assert.equal(host.closed,false);
 await host.rpc({method:'start',run_id:'second'});assert.notEqual(host.child.pid,old.pid);
});
test('idle release refuses while any RPC remains pending',async t=>{
 const f=fixture(t);const pending=f.host.rpc({method:'start',run_id:'run'});assert.equal(f.host.releaseIdle(),false);assert.notEqual(f.host.child,null);
 f.child.stdout.write(JSON.stringify({id:f.requests[0].id,result:{}})+'\n');await pending;assert.equal(f.host.releaseIdle(),true);
});
test('late released child exit cannot invalidate a new child',async t=>{
 const f=fixture(t);let crashes=0;f.host.onCrash=()=>{crashes++;};
 const first=f.host.rpc({method:'start',run_id:'first'});f.child.stdout.write(JSON.stringify({id:f.requests.at(-1).id,result:{}})+'\n');await first;const old=f.child;assert.equal(f.host.releaseIdle(),true);
 const second=f.host.rpc({method:'start',run_id:'second'});const current=f.child;old.emit('exit',0);assert.equal(f.host.child,current);assert.equal(crashes,0);
 current.stdout.write(JSON.stringify({id:f.requests.at(-1).id,result:{}})+'\n');await second;
});
