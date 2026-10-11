// Executes the installed monitor source after our pinned transformation. Only
// transport, account admission and ordinary message dispatch are test doubles.
import vm from 'node:vm';
import assert from 'node:assert/strict';
import {readFileSync,mkdtempSync,mkdirSync,rmSync}from'node:fs';
import{join}from'node:path';import{tmpdir}from'node:os';
import{transformWeixinMonitor}from'./patch-archive-ingress.mjs';
import{ArchiveInbox}from'../adapters/openclaw/archive-ingress.mjs';
import{ArchiveBridge,BRIDGE_SYMBOL}from'../adapters/openclaw/archive-bridge.mjs';
const pkg=process.argv[2];const source=transformWeixinMonitor(readFileSync(join(pkg,'dist/src/monitor/monitor.js'),'utf8'),false);
const dir=mkdtempSync(join(tmpdir(),'atrium-native-monitor-'));mkdirSync(join(dir,'inbox'),{mode:0o700});const inbox=new ArchiveInbox({root:join(dir,'inbox'),scope:'synthetic'});
const raw={message_id:'native-one',from_user_id:'synthetic',item_list:[{type:1,text_item:{text:'不要在 TV 展示'}},{type:2,image_item:{media:{full_url:'https://novac2c.cdn.weixin.qq.com/a'}}},{type:4,file_item:{file_name:'report.txt',media:{full_url:'https://novac2c.cdn.weixin.qq.com/b'}}}]};
const bridge=new ArchiveBridge({inbox,core:{lookup:async()=>null,upload:async()=>{throw Error('offline');}},download:async function*(){yield Buffer.from('original');}});
let saved=0,dispatched=0,crash=true;const originalCapture=bridge.captureWeixinBatch.bind(bridge);let abort;
bridge.captureWeixinBatch=async(...args)=>{const result=await originalCapture(...args);if(crash){abort.abort();throw Error('synthetic_crash_after_capture');}return result;};
async function monitor(){abort=new AbortController();const context=vm.createContext({Symbol,Date,Error,Promise,setTimeout,clearTimeout,AbortController,console});context[BRIDGE_SYMBOL]=bridge;
 const quiet={info(){},debug(){},error(){}};
 const modules={
 '../api/api.js':{getUpdates:async()=>({msgs:[raw],get_updates_buf:'synthetic-cursor'}),classifyFetchError:()=>({type:'synthetic',description:'synthetic'})},
 '../api/config-cache.js':{WeixinConfigManager:class{async getForUser(){return {};}}},
 '../api/session-guard.js':{STALE_TOKEN_ERRCODE:-1,pauseSession(){},getRemainingPauseMs:()=>0},
 '../messaging/process-message.js':{atriumArchiveAdmission:async()=>true,processOneMessage:async full=>{assert.equal(full.message_id,raw.message_id);assert.ok(bridge.current()?.ref);assert.equal(inbox.describe(bridge.current().ref).slots.length,2);dispatched++;abort.abort();}},
 '../storage/sync-buf.js':{getSyncBufFilePath:()=>'/unused',loadGetUpdatesBuf:()=>'',saveGetUpdatesBuf:()=>{saved++;}},
 '../util/logger.js':{logger:{...quiet,withAccount:()=>quiet}},'../util/redact.js':{redactBody:()=>'<redacted>'},
 };
 const mod=new vm.SourceTextModule(source,{context});await mod.link(async specifier=>{const exports=modules[specifier];assert.ok(exports,`unexpected dependency ${specifier}`);const m=new vm.SyntheticModule(Object.keys(exports),function(){for(const[k,v]of Object.entries(exports))this.setExport(k,v);},{context});return m;});await mod.evaluate();await mod.namespace.monitorWeixinProvider({accountId:'account',baseUrl:'https://example.invalid',cdnBaseUrl:'https://novac2c.cdn.weixin.qq.com',config:{},channelRuntime:{},abortSignal:abort.signal});
}
try{await monitor();assert.equal(saved,0);assert.equal(dispatched,0);assert.equal(inbox.deliveries('openclaw-weixin','account').length,1);crash=false;await monitor();assert.equal(saved,1);assert.equal(dispatched,1);assert.equal(inbox.deliveries('openclaw-weixin','account').length,0);assert.equal(inbox.db.prepare('SELECT count(*) n FROM messages').get().n,1);console.log(JSON.stringify({native_monitor:true,capture_before_cursor:true,crash_replayed_once:true,slots:2}));}finally{await bridge.stop();inbox.close();rmSync(dir,{recursive:true,force:true});}
