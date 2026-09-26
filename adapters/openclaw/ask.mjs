#!/usr/bin/env node
import { spawn } from 'node:child_process';
import { randomUUID } from 'node:crypto';
import { mkdtempSync,chmodSync,openSync,closeSync,fstatSync,readSync,rmSync,lstatSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join,isAbsolute } from 'node:path';
import { pathToFileURL } from 'node:url';
import { renderReceipts,readReceiptPrefix } from './receipt-renderer.mjs';

export async function runAsk(config,{deadlineMs=30000,killGraceMs=1000,spawnChild=spawn}={}){
 if(!isAbsolute(config.config??'')||!isAbsolute(config.openclawBin??'/opt/homebrew/bin/openclaw')||typeof config.agent!=='string'||!/^[A-Za-z0-9_-]{1,128}$/.test(config.agent)||typeof config.message!=='string'||!config.message.trim()||Buffer.byteLength(config.message)>32768)throw new Error('invalid_ask_arguments');
 const dir=mkdtempSync(join(tmpdir(),'atrium-ask-'));chmodSync(dir,0o700);const file=join(dir,'evidence.jsonl'),fd=openSync(file,'wx+',0o600),stat=fstatSync(fd);
 const invocation=randomUUID(),session=randomUUID(),started=Date.now();let receipts=[],invalid=false,timedOut=false,overflow=false,exitCode=null;const stdoutChunks=[];
 try{
  await new Promise(resolve=>{
   let child,finished=false,killTimer;
   const finish=()=>{if(finished)return;finished=true;clearTimeout(timer);clearTimeout(killTimer);process.removeListener('SIGINT',interrupt);process.removeListener('SIGTERM',interrupt);resolve();};
   const killGroup=signal=>{if(child?.pid)try{process.kill(-child.pid,signal);}catch{/* already exited */}};
   const stop=()=>{killGroup('SIGTERM');if(!killTimer)killTimer=setTimeout(()=>{killGroup('SIGKILL');finish();},killGraceMs);};
   const interrupt=()=>{timedOut=true;stop();};
   const timer=setTimeout(()=>{timedOut=true;stop();},deadlineMs);
   process.on('SIGINT',interrupt);process.on('SIGTERM',interrupt);
   try{child=spawnChild(config.openclawBin??'/opt/homebrew/bin/openclaw',['agent','--local','--agent',config.agent,'--session-id',session,'--message',config.message,'--json','--thinking','off','--timeout','30'],{shell:false,detached:true,stdio:['ignore','pipe','pipe'],env:{...process.env,OPENCLAW_CONFIG_PATH:config.config,ATRIUM_BRAIN_RECEIPT_FILE:file,ATRIUM_BRAIN_INVOCATION_ID:invocation}});}catch{finish();return;}
   let outputBytes=0;
   const discard=chunk=>{outputBytes+=chunk.length;if(outputBytes>1024*1024&&!overflow){overflow=true;stop();}};
   child.stdout.on('data',chunk=>{discard(chunk);if(!overflow)stdoutChunks.push(chunk);});child.stderr.on('data',discard);
   child.on('error',finish);child.on('exit',code=>{exitCode=code;});
   child.on('close',()=>{if(!killTimer){killGroup('SIGKILL');finish();}});
   // Keep the deadline if a grandchild holds inherited pipes after leader exit.
  });
  const after=fstatSync(fd),path=lstatSync(file);
  if(after.dev!==stat.dev||after.ino!==stat.ino||path.ino!==stat.ino||path.dev!==stat.dev||!path.isFile()||path.isSymbolicLink()||(path.mode&0o077)!==0)throw new Error('invalid_receipt_file');
  const buffer=Buffer.alloc(Math.min(after.size,512*1024));const length=readSync(fd,buffer,0,buffer.length,0);
  const parsed=readReceiptPrefix(buffer.subarray(0,length).toString('utf8'),invocation);receipts=parsed.receipts;invalid=parsed.invalid||after.size>512*1024;
 }catch{invalid=true;}finally{closeSync(fd);rmSync(dir,{recursive:true,force:true});}
 let metadataValid=false,metadataIncomplete=true;
 try{const meta=JSON.parse(Buffer.concat(stdoutChunks).toString('utf8'))?.meta;
  // OpenClaw 2026.5.20 omits toolSummary for a completed zero-tool turn.
  const noTools=meta&& !Object.hasOwn(meta,'toolSummary') && meta.agentMeta?.sessionId===session && meta.aborted===false && meta.completion?.stopReason==='stop' && receipts.length===0;
  const value=noTools?{calls:0,failures:0}:meta?.toolSummary;
  metadataValid=!!value&&Number.isSafeInteger(value.calls)&&value.calls>=0&&Number.isSafeInteger(value.failures)&&value.failures>=0&&value.failures<=value.calls;
  metadataIncomplete=!metadataValid||value.calls!==receipts.length||value.failures>0;
 }catch{/* Raw model output is never rendered. */}
 const failed=invalid||overflow||exitCode!==0||metadataIncomplete;
 return {text:renderReceipts(receipts,{failed,timedOut}),receipts,elapsed_ms:Date.now()-started,failed,timed_out:timedOut};
}
function parse(argv){const out={agent:'atrium-home',openclawBin:'/opt/homebrew/bin/openclaw',json:false};const map={'--config':'config','--openclaw-bin':'openclawBin','--agent':'agent','--message':'message'};for(let i=0;i<argv.length;i++){const key=argv[i];if(key==='--json'){out.json=true;continue;}if(!Object.hasOwn(map,key)||i+1===argv.length)throw new Error('invalid_ask_arguments');out[map[key]]=argv[++i];}return out;}
if(process.argv[1]&&import.meta.url===pathToFileURL(process.argv[1]).href){try{const args=parse(process.argv.slice(2)),result=await runAsk(args);process.stdout.write((args.json?JSON.stringify(result):result.text)+'\n');if(result.failed||result.timed_out)process.exitCode=1;}catch{process.stderr.write('用法：ask.mjs --config /绝对路径/openclaw.json --message 提问 [--agent atrium-home] [--openclaw-bin /绝对路径/openclaw] [--json]\n');process.exitCode=2;}}
