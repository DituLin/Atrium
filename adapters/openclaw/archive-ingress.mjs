// Trusted native input boundary. Never import this module into a model sandbox.
import { spawn } from 'node:child_process';
import { PassThrough } from 'node:stream';
import { DatabaseSync } from 'node:sqlite';
import { createHash, randomUUID } from 'node:crypto';
import { constants, openSync, closeSync, fstatSync, lstatSync, mkdirSync, fsyncSync, renameSync, unlinkSync, writeFileSync } from 'node:fs';
import { open } from 'node:fs/promises';
import { join, resolve, relative, sep, dirname } from 'node:path';
export const MAX_ARCHIVE_BYTES=32*1024*1024;
const fail=code=>Object.assign(new Error(`archive_${code}`),{code:`archive_${code}`});
const hash=v=>createHash('sha256').update(typeof v==='string'||Buffer.isBuffer(v)?v:JSON.stringify(v)).digest('hex');
const text=v=>typeof v==='string' && v.length>0 && Buffer.byteLength(v)<=512 && !/[\x00-\x1f\x7f]/.test(v);
function identity(channel,account,conversation,sender,message){if(![account,conversation,sender,message].every(text))throw fail('identity');return {channel,account,conversation,sender,provider_message_id:message};}
function finish(identity,caption,slots){if(typeof caption!=='string'||Buffer.byteLength(caption)>32768||slots.length>32)throw fail('bounds');const m={identity,caption,slots};return {...m,digest:hash(m)};}
export function normalizeWeixin(raw,ctx){
 if(ctx?.admitted!==true)throw fail('admission');
 const id=identity('openclaw-weixin',ctx.account,raw.from_user_id,raw.from_user_id,raw.message_id);
 if(!Array.isArray(raw.item_list)||raw.item_list.length>128)throw fail('bounds');
 const caption=raw.item_list.filter(i=>i.type===1&&typeof i.text_item?.text==='string').map(i=>i.text_item.text).join('\n');
 const slots=[];
 raw.item_list.forEach((item,index)=>{const field=({2:'image_item',3:'voice_item',4:'file_item',5:'video_item'})[item.type];if(!field)return;const media=item[field]??{};
  slots.push({index,attachment_id:`item:${index}`,name:media.file_name??`attachment-${index}.${item.type===2?'image':item.type===3?'silk':item.type===5?'mp4':'bin'}`,mime:({2:'application/octet-stream',3:'audio/silk',5:'video/mp4'})[item.type]??'application/octet-stream',provider:{kind:'weixin',media:media.media??{},aeskey:media.aeskey??'',md5:media.md5??media.rawfilemd5??'',size:media.len??media.size??null}});
 });
 return finish(id,caption,slots);
}
export function normalizeDiscord(raw,ctx){
 if(ctx?.admitted!==true)throw fail('admission');
 const id=identity('discord',ctx.account,raw.channel_id,raw.author?.id,raw.id);
 if(![raw.id,raw.channel_id,raw.author?.id].every(v=>/^\d{10,24}$/.test(v)))throw fail('identity');
 if(!Array.isArray(raw.attachments)||raw.attachments.length>32)throw fail('bounds');
 return finish(id,raw.content??'',raw.attachments.map((a,index)=>{if(!/^\d{10,24}$/.test(a.id))throw fail('identity');return {index,attachment_id:a.id,name:a.filename??`attachment-${index}.bin`,mime:a.content_type??'application/octet-stream',provider:{kind:'discord',url:a.url,size:a.size??null}};}));
}
function privateDir(path){const st=lstatSync(path);if(!st.isDirectory()||(st.mode&0o777)!==0o700)throw fail('directory');}
function syncDirectory(path){const fd=openSync(path,constants.O_RDONLY|constants.O_NOFOLLOW);try{fsyncSync(fd);}finally{closeSync(fd);}}
export class ArchiveInbox {
 constructor({root,scope,maxMessages=1024,maxBytes=1024*1024*1024,sourceReader,now=Date.now}){
  this.sourceReader=sourceReader;this.root=resolve(root);privateDir(this.root);this.now=now;this.maxMessages=maxMessages;this.maxBytes=maxBytes;
  for(const name of ['spool']){try{mkdirSync(join(root,name),{mode:0o700});}catch(e){if(e.code!=='EEXIST')throw e;}privateDir(join(root,name));}
  const dbpath=join(root,'inbox.sqlite');try{const fd=openSync(dbpath,constants.O_CREAT|constants.O_EXCL|constants.O_RDWR|constants.O_NOFOLLOW,0o600);closeSync(fd);}catch(e){if(e.code!=='EEXIST')throw e;}
  const st=lstatSync(dbpath);if(!st.isFile()||(st.mode&0o777)!==0o600)throw fail('database');
  this.db=new DatabaseSync(dbpath);this.db.exec(`PRAGMA secure_delete=ON; PRAGMA journal_mode=WAL; PRAGMA synchronous=FULL; PRAGMA busy_timeout=5000;
 CREATE TABLE IF NOT EXISTS meta(singleton INTEGER PRIMARY KEY,scope TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS messages(ref TEXT PRIMARY KEY,identity TEXT UNIQUE NOT NULL,digest TEXT NOT NULL,caption TEXT NOT NULL,intent TEXT NOT NULL,created INTEGER NOT NULL,expires INTEGER NOT NULL,raw TEXT NOT NULL,processed INTEGER NOT NULL DEFAULT 0);
 CREATE TABLE IF NOT EXISTS slots(ref TEXT NOT NULL,idx INTEGER NOT NULL,attachment_id TEXT NOT NULL,name TEXT NOT NULL,mime TEXT NOT NULL,provider TEXT NOT NULL,spool_released INTEGER NOT NULL DEFAULT 0,kind TEXT NOT NULL DEFAULT 'file',state TEXT NOT NULL DEFAULT 'downloading',sha256 TEXT,size INTEGER,attempts INTEGER NOT NULL DEFAULT 0,next_attempt INTEGER NOT NULL DEFAULT 0,error TEXT,receipt TEXT,PRIMARY KEY(ref,idx));`);
  this.db.prepare('INSERT OR IGNORE INTO meta VALUES(1,?)').run(scope);if(this.db.prepare('SELECT scope FROM meta').get()?.scope!==scope){this.db.close();throw fail('scope');}
 }
 close(){this.db.close();}
 tx(fn){this.db.exec('BEGIN IMMEDIATE');try{const r=fn();this.db.exec('COMMIT');return r;}catch(e){this.db.exec('ROLLBACK');throw e;}}
 captureBatch(manifests){if(!Array.isArray(manifests)||manifests.length>256)throw fail('bounds');return this.tx(()=>{
  const refs=[];for(const m of manifests){const id=JSON.stringify(m.identity);const prior=this.db.prepare('SELECT ref,digest FROM messages WHERE identity=?').get(id);if(prior){if(prior.digest!==m.digest)throw fail('conflict');refs.push(prior.ref);continue;}
   const n=this.db.prepare("SELECT count(*) n FROM messages WHERE processed=0 OR ref IN(SELECT ref FROM slots WHERE state NOT IN ('core_saved','terminal'))").get().n;if(n>=this.maxMessages)throw fail('quota');
   if(Buffer.byteLength(JSON.stringify(m))>65536)throw fail('bounds');const ref=randomUUID();
   this.db.prepare('INSERT INTO messages(ref,identity,digest,caption,intent,created,expires,raw) VALUES(?,?,?,?,?,?,?,?)').run(ref,id,m.digest,m.caption,m.caption.trim()?'awaiting_intent':'inherit',this.now(),this.now()+24*3600_000,JSON.stringify(m.raw??null));
   for(const s of m.slots){if(!Number.isInteger(s.index)||s.index<0||s.index>127||!text(s.name)||!text(s.mime))throw fail('slot');this.db.prepare('INSERT INTO slots(ref,idx,attachment_id,name,mime,provider) VALUES(?,?,?,?,?,?)').run(ref,s.index,s.attachment_id,s.name,s.mime,JSON.stringify(s.provider));}refs.push(ref);
  }return refs;
 });}
 expire(){this.tx(()=>{this.db.prepare("UPDATE messages SET raw='',processed=1 WHERE expires<=?").run(this.now());this.db.prepare("UPDATE messages SET caption='' WHERE expires<=? AND ref NOT IN(SELECT ref FROM slots)").run(this.now());this.db.prepare("UPDATE slots SET provider='{}' WHERE ref IN(SELECT ref FROM messages WHERE expires<=?)").run(this.now());this.db.prepare("UPDATE slots SET provider='{}',state='terminal',error='archive_expired' WHERE state='downloading' AND ref IN(SELECT ref FROM messages WHERE expires<=?)").run(this.now());});this.db.exec('PRAGMA wal_checkpoint(TRUNCATE)');for(const s of this.db.prepare("SELECT ref,idx FROM slots WHERE state='core_saved' AND spool_released=0 LIMIT 128").all()){this.releaseSpool(s.ref,s.idx)}}
 deliveries(channel,account){return this.db.prepare("SELECT ref,raw FROM messages WHERE processed=0 AND expires>? AND json_extract(identity,'$.channel')=? AND json_extract(identity,'$.account')=? ORDER BY created,rowid LIMIT 256").all(this.now(),channel,account).map(m=>({ref:m.ref,raw:JSON.parse(m.raw)}));}
 processed(ref){this.db.prepare("UPDATE messages SET raw='',processed=1,caption=CASE WHEN ref NOT IN(SELECT ref FROM slots) THEN '' ELSE caption END WHERE ref=?").run(ref);this.db.exec('PRAGMA wal_checkpoint(TRUNCATE)');}
 manifest(ref){const m=this.db.prepare('SELECT identity,caption,digest,intent FROM messages WHERE ref=?').get(ref);if(!m)throw fail('missing');return {...m,identity:JSON.parse(m.identity)};}
 describe(ref){const m=this.db.prepare('SELECT ref,digest,intent FROM messages WHERE ref=?').get(ref);if(!m)throw fail('missing');return {...m,slots:this.db.prepare('SELECT idx AS slot_index,name,mime,state,sha256,size,error,receipt FROM slots WHERE ref=? ORDER BY idx').all(ref).map(s=>({...s,receipt:s.receipt?JSON.parse(s.receipt):null}))};}
 pending(){return this.db.prepare("SELECT ref FROM messages WHERE expires>? AND ref IN(SELECT ref FROM slots WHERE state='downloading' AND next_attempt<=? AND attempts<32) ORDER BY created,rowid LIMIT 32").all(this.now(),this.now()).map(x=>x.ref);}
 setIntent(ref,intent,digest){if(!['inherit','private','needs_clarification'].includes(intent))throw fail('intent');return this.tx(()=>{const m=this.db.prepare('SELECT * FROM messages WHERE ref=?').get(ref);if(!m||m.digest!==digest)throw fail('identity');if(m.intent!==intent&&!['awaiting_intent','needs_clarification'].includes(m.intent))throw fail('conflict');this.db.prepare('UPDATE messages SET intent=? WHERE ref=?').run(intent,ref);return this.describe(ref);});}
 async stageText(ref){const m=this.manifest(ref);if(!m.caption.trim())throw fail('text');this.db.prepare("INSERT OR IGNORE INTO slots(ref,idx,attachment_id,name,mime,provider,kind) VALUES(?,1023,'original-text','message.txt','text/plain; charset=utf-8','{}','text')").run(ref);return this.stageStream(ref,1023,[Buffer.from(m.caption)]);}
 async stageFile(ref,index,path,roots){
  const absolute=resolve(path);const root=roots.map(r=>resolve(r)).find(r=>{const rel=relative(r,absolute);return rel&&!rel.startsWith(`..${sep}`)&&rel!=='..'&&!rel.startsWith(sep);});if(!root)throw fail('source');
  if(!this.sourceReader)throw fail('source');
  const child=spawn(this.sourceReader,['--root',root,'--relative',relative(root,absolute)],{stdio:['ignore','pipe','ignore'],shell:false});
  const source=new PassThrough();child.stdout.pipe(source);
  const completed=new Promise((done,reject)=>{child.on('error',()=>reject(fail('source')));child.on('exit',code=>code===0?done():reject(fail('source')));});completed.catch(()=>{});
  const timer=setTimeout(()=>child.kill('SIGKILL'),120000);
  async function* verified(){for await(const bytes of source)yield bytes;await completed;}
  try{return await this.stageStream(ref,index,verified());}finally{clearTimeout(timer);child.kill();}
 }
 async stageStream(ref,index,stream){const slot=this.db.prepare('SELECT * FROM slots WHERE ref=? AND idx=?').get(ref,index);if(!slot)throw fail('slot');const temp=join(this.root,'spool',`.${randomUUID()}`);const fd=await open(temp,constants.O_WRONLY|constants.O_CREAT|constants.O_EXCL|constants.O_NOFOLLOW,0o600);let size=0;const digest=createHash('sha256');try{
   for await(const chunk of stream){size+=chunk.length;if(size>MAX_ARCHIVE_BYTES)throw fail('too_large');digest.update(chunk);await fd.writeFile(chunk);}if(size===0)throw fail('empty');await fd.sync();await fd.close();const sha=digest.digest('hex');
   this.tx(()=>{const current=this.db.prepare('SELECT * FROM slots WHERE ref=? AND idx=?').get(ref,index);if(current.sha256&&(current.sha256!==sha||current.size!==size))throw fail('conflict');
    const used=this.db.prepare("SELECT coalesce(sum(size),0) n FROM slots WHERE state!='core_saved'").get().n;if(!current.sha256&&used+size>this.maxBytes)throw fail('quota');
    const final=join(this.root,'spool',`${ref}-${index}`);renameSync(temp,final);syncDirectory(join(this.root,'spool'));
    this.db.prepare("UPDATE slots SET sha256=?,size=?,state=CASE WHEN state='core_saved' THEN state ELSE 'staged' END,provider='{}',error=NULL,attempts=0,next_attempt=0 WHERE ref=? AND idx=?").run(sha,size,ref,index);
   });return this.describe(ref);
  }finally{try{await fd.close();}catch{}try{unlinkSync(temp);}catch{}}
 }
 releaseSpool(ref,index){try{unlinkSync(join(this.root,'spool',`${ref}-${index}`));syncDirectory(join(this.root,'spool'));}catch(e){if(e.code!=='ENOENT')return;}this.db.prepare('UPDATE slots SET spool_released=1 WHERE ref=? AND idx=?').run(ref,index);}
 async reconcile(core,{force=false,signal}={}) {
  if(this.reconciling)return;this.reconciling=true;
  try {const rows=this.db.prepare("SELECT s.*,m.identity,m.intent FROM slots s JOIN messages m ON m.ref=s.ref WHERE s.state IN ('staged','submitting') AND m.intent IN ('inherit','private') AND s.attempts<32 AND (? OR s.next_attempt<=?) ORDER BY s.next_attempt LIMIT 8").all(force?1:0,this.now());
   for(const s of rows){if(signal?.aborted)break;const source={...JSON.parse(s.identity),kind:s.kind,slot_index:s.idx};
    try {const bytes=await this.openStaged(s.ref,s.idx);this.db.prepare("UPDATE slots SET state='submitting',attempts=attempts+1,next_attempt=? WHERE ref=? AND idx=?").run(this.now()+Math.min(3600_000,1000*2**Math.min(s.attempts,12)),s.ref,s.idx);
     let receipt=await core.lookup(source,signal);if(!receipt)receipt=await core.upload({...source,original_name:s.name,declared_mime:s.mime,expected_size:s.size,expected_sha256:s.sha256,visibility_intent:s.intent},bytes,signal);
     const projected=validateIntakeReceipt(receipt,s);
     this.db.prepare("UPDATE slots SET state='core_saved',receipt=?,error=NULL,provider='{}' WHERE ref=? AND idx=?").run(JSON.stringify(projected),s.ref,s.idx);
     this.releaseSpool(s.ref,s.idx)
    }catch(e){const code=['archive_forbidden','archive_conflict','archive_spool','archive_receipt','archive_too_large'].includes(e?.code)?e.code:'archive_retry';this.db.prepare("UPDATE slots SET error=?,state=CASE WHEN ? IN ('archive_conflict','archive_spool','archive_receipt','archive_too_large') THEN 'terminal' ELSE state END WHERE ref=? AND idx=?").run(code,code,s.ref,s.idx);}
   }
  }finally{this.reconciling=false;}
 }
 async openStaged(ref,index){const s=this.db.prepare('SELECT * FROM slots WHERE ref=? AND idx=?').get(ref,index);if(!s?.sha256)throw fail('slot');const fd=await open(join(this.root,'spool',`${ref}-${index}`),constants.O_RDONLY|constants.O_NOFOLLOW);try{const st=await fd.stat();if(!st.isFile()||st.size!==s.size||st.size>MAX_ARCHIVE_BYTES)throw fail('spool');const b=await fd.readFile();if(hash(b)!==s.sha256)throw fail('spool');return b;}finally{await fd.close();}}
}

export function validateIntakeReceipt(r,s){
 const id=v=>typeof v==='string'&&/^[0-7][0-9A-HJKMNP-TV-Z]{25}$/.test(v);
 if(!r||!id(r.receipt_id)||!id(r.task_id)||r.original_status!=='saved'||r.sha256!==s.sha256||r.size_bytes!==s.size||typeof r.received_at!=='string'||!Number.isFinite(Date.parse(r.received_at)))throw fail('receipt');
 return {receipt_id:r.receipt_id,task_id:r.task_id,original_status:'saved',sha256:r.sha256,size_bytes:r.size_bytes,received_at:r.received_at};
}
