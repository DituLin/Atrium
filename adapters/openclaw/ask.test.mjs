import test from 'node:test';
import assert from 'node:assert/strict';
import { runAsk } from './ask.mjs';
import { renderReceipts, validateReceipts, readReceiptPrefix } from './receipt-renderer.mjs';
import { randomUUID } from 'node:crypto';
const invocation=randomUUID();const stamp='2026-09-07T10:00:00+08:00';
function receipt(tool,data,extra={}){return {schema_version:1,invocation_id:invocation,run_id:'a'.repeat(64),tool,arguments:{},tool_result:{structuredContent:{schema_version:'1',observed_at:stamp,availability:'available',data}},...extra};}
test('state renderer preserves degradation and exact observation timezone',()=>{
 const text=renderReceipts([receipt('home_get_status',{core:'reachable',nas:[{id:'nas',health:'degraded'}],screens:[{id:'tv',online:false}]})]);
 assert.match(text,/degraded/);assert.match(text,/2026-09-07T10:00:00\+08:00/);assert.doesNotMatch(text,/全部正常|关机|挂载失败/);
});
test('empty baseline photo page is not todays newly added count',()=>{
 const text=renderReceipts([receipt('home_list_photos',{collection:'captured_today',items:[],baseline_only:true})]);assert.match(text,/本页.*0/);assert.match(text,/基线/);assert.doesNotMatch(text,/今天新增.*0/);
});
test('multiple screens without action asks for a target',()=>{
 assert.match(renderReceipts([receipt('home_list_screens',[{id:'one',online:true},{id:'two',online:true}])]),/指定.*屏幕/);
});
test('wrong invocation and malformed top-level fields rejected',()=>{
 assert.throws(()=>validateReceipts(JSON.stringify(receipt('home_get_status',{}))+'\n',randomUUID()),/invalid_receipt/);
 assert.throws(()=>validateReceipts(JSON.stringify({...receipt('home_get_status',{}),secret:'x'})+'\n',invocation),/invalid_receipt/);
});
const command='01K4D6H0000000000000000002',operation='01K4D6H0000000000000000001';
function actionReceipt(status,when=stamp){const r=receipt('home_refresh_screen',{id:command,screen_id:'tv',status,kind:'refresh',payload:{}},{arguments:{screen_id:'tv'},report:{Status:status,ScreenID:'tv',CommandID:command,OperationID:operation,ObservedAt:when,TextZH:'模型声称一切正常'}});r.tool_result.structuredContent.observed_at=when;return r;}
test('applied cannot be overwritten by accepted and unknown is never confirmation',()=>{
 const text=renderReceipts([actionReceipt('applied'),actionReceipt('accepted','2026-09-07T10:00:01+08:00')]);assert.match(text,/确认执行/);assert.doesNotMatch(text,/尚未确认|模型声称/);
 const unknown=actionReceipt('unknown');unknown.tool_result.isError=true;assert.match(renderReceipts([unknown]),/结果未知/);assert.doesNotMatch(renderReceipts([unknown]),/已在/);
});
test('failure retains trusted IDs but no receipt cannot imply nothing happened',()=>{
 const r=receipt('home_refresh_screen',null,{tool_result:null,error:{code:'outcome_unknown',operation_id:operation,command_id:command}});
 const text=renderReceipts([r],{failed:true});assert.match(text,new RegExp(operation));assert.match(text,/未取得可核实/);
 assert.doesNotMatch(renderReceipts([],{timedOut:true}),/未执行|没有执行/);
});
import { mkdtempSync,writeFileSync,rmSync,readFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
function executable(t,source){const dir=mkdtempSync(join(tmpdir(),'atrium-ask-test-'));t.after(()=>rmSync(dir,{recursive:true,force:true}));const file=join(dir,'fake-openclaw');writeFileSync(file,'#!'+process.execPath+'\n'+source,{mode:0o700});return {file,dir};}
test('actual child model hallucination is discarded and private receipt is rendered',async t=>{
 const {file}=executable(t,`const fs=require('fs');const r=${JSON.stringify(receipt('home_get_status',{core:'reachable'}))};r.invocation_id=process.env.ATRIUM_BRAIN_INVOCATION_ID;const mode=fs.statSync(process.env.ATRIUM_BRAIN_RECEIPT_FILE).mode&511;if(mode!==384)process.exit(2);fs.appendFileSync(process.env.ATRIUM_BRAIN_RECEIPT_FILE,JSON.stringify(r)+'\\n');console.log(JSON.stringify({payloads:[{text:'幻觉：全部正常，已切换电视'}],meta:{toolSummary:{calls:1,failures:0}}}));`);
 const result=await runAsk({config:'/private/config.json',openclawBin:file,agent:'atrium-home',message:'status'});assert.equal(result.failed,false);assert.match(result.text,/core=reachable/);assert.doesNotMatch(JSON.stringify(result),/幻觉|全部正常/);assert.equal(result.receipts.length,1);
});
test('wrong invocation from child rejected without leaking raw output',async t=>{
 const {file}=executable(t,`const fs=require('fs');fs.appendFileSync(process.env.ATRIUM_BRAIN_RECEIPT_FILE,JSON.stringify(${JSON.stringify(receipt('home_get_status',{core:'reachable'}))})+'\\n');console.log('private model secret');`);
 const result=await runAsk({config:'/private/config.json',openclawBin:file,agent:'atrium-home',message:'status'});assert.equal(result.failed,true);assert.equal(result.receipts.length,0);assert.doesNotMatch(JSON.stringify(result),/private model secret/);
});
test('timeout kills own process group including TERM-ignoring grandchild',async t=>{
 const {file,dir}=executable(t,`const cp=require('child_process'),fs=require('fs');process.on('SIGTERM',()=>{});const child=cp.spawn(process.execPath,['-e',"process.on('SIGTERM',()=>{});setInterval(()=>{},1000)"],{stdio:'ignore'});setInterval(()=>{},1000);`);
 // The group leader is observed directly through an injected spawn wrapper.
 let pid;const {spawn}=await import('node:child_process');
 const result=await runAsk({config:'/private/config.json',openclawBin:file,agent:'atrium-home',message:'status'},{deadlineMs:150,killGraceMs:100,spawnChild(...args){const c=spawn(...args);pid=c.pid;return c;}});
 assert.equal(result.timed_out,true);await new Promise(r=>setTimeout(r,50));assert.throws(()=>process.kill(-pid,0));
});
test('all negative terminal receipts survive later accepted with conservative conflict note',()=>{
 for(const status of ['failed','expired','unknown']){const r=actionReceipt(status);r.tool_result.isError=true;r.error={code:'tool_error',operation_id:operation,command_id:command};const text=renderReceipts([r,actionReceipt('accepted','2026-09-07T10:01:00+08:00')]);assert.match(text,/2026-09-07T10:00:00\+08:00/);assert.match(text,/保留上述原观察结论/);assert.doesNotMatch(text,/命令已接受/);}
 const transport=actionReceipt('failed');transport.tool_result.isError=true;transport.error={code:'outcome_unknown'};assert.match(renderReceipts([transport]),/未取得可核实/);
});
test('valid applied prefix survives interrupted receipt line and returns failure',async t=>{
 const {file}=executable(t,`const fs=require('fs');const r=${JSON.stringify(actionReceipt('applied'))};r.invocation_id=process.env.ATRIUM_BRAIN_INVOCATION_ID;fs.appendFileSync(process.env.ATRIUM_BRAIN_RECEIPT_FILE,JSON.stringify(r)+'\\n{');console.log('model final invented');`);
 const result=await runAsk({config:'/private/config.json',openclawBin:file,agent:'atrium-home',message:'status'});assert.equal(result.failed,true);assert.equal(result.receipts.length,1);assert.match(result.text,/刷新屏幕.*确认执行/);assert.match(result.text,new RegExp(command));assert.equal(typeof result.elapsed_ms,'number');assert.equal(result.model_elapsed_ms,undefined);
});
test('capability boundary names unsupported features even when only a photo list ran',()=>{
 const text=renderReceipts([receipt('home_list_photos',{collection:'recent',items:[],baseline_only:true})]);assert.match(text,/暂无非基线最近新增照片/);assert.match(text,/视频、人物搜索与日历尚未接入/);assert.doesNotMatch(text,/本页为基线内容/);
});
test('successful CLI with more tool calls than receipts reports unknown trailing action',async t=>{
 const {file}=executable(t,`const fs=require('fs');const r=${JSON.stringify(actionReceipt('applied'))};r.invocation_id=process.env.ATRIUM_BRAIN_INVOCATION_ID;fs.appendFileSync(process.env.ATRIUM_BRAIN_RECEIPT_FILE,JSON.stringify(r)+'\\n');console.log(JSON.stringify({meta:{toolSummary:{calls:2,failures:0}},payloads:[{text:'全部动作完成'}]}));`);
 const result=await runAsk({config:'/private/config.json',openclawBin:file,agent:'atrium-home',message:'status'});assert.equal(result.failed,true);assert.equal(result.receipts.length,1);assert.match(result.text,/可能还有未取得回执的动作/);assert.match(result.text,/--recover/);assert.match(result.text,new RegExp(command));assert.doesNotMatch(JSON.stringify(result),/全部动作完成/);
});
test('JSON-complete last line without LF is not committed evidence',()=>{
 const first=receipt('home_get_status',{core:'reachable'}),last=actionReceipt('applied');
 const result=readReceiptPrefix(JSON.stringify(first)+'\n'+JSON.stringify(last),invocation);
 assert.equal(result.invalid,true);assert.equal(result.receipts.length,1);assert.doesNotMatch(renderReceipts(result.receipts,{failed:true}),/确认执行/);
 assert.equal(readReceiptPrefix(JSON.stringify(last),invocation).receipts.length,0);
});
test('completed OpenClaw turn omits toolSummary when no tools were called',async t=>{
 const {file}=executable(t,`const args=process.argv;const session=args[args.indexOf('--session-id')+1];console.log(JSON.stringify({meta:{agentMeta:{sessionId:session},aborted:false,completion:{stopReason:'stop'}},payloads:[{text:'假的：视频已播放'}]}));`);
 const result=await runAsk({config:'/private/config.json',openclawBin:file,agent:'atrium-home',message:'播放视频'});
 assert.equal(result.failed,false);assert.equal(result.receipts.length,0);assert.match(result.text,/视频、人物搜索与日历尚未接入/);assert.doesNotMatch(result.text,/视频已播放/);
});
