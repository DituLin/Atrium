import test from 'node:test';
import { execFileSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import assert from 'node:assert/strict';
import { mkdtempSync, mkdirSync, writeFileSync, readFileSync, rmSync, symlinkSync, realpathSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { ArchiveInbox, normalizeWeixin, normalizeDiscord } from './archive-ingress.mjs';
function setup(t, options={}) { const dir=realpathSync(mkdtempSync(join(tmpdir(),'atrium-ingress-'))); t.after(()=>rmSync(dir,{recursive:true,force:true})); const root=join(dir,'inbox');mkdirSync(root,{mode:0o700}); const inbox=new ArchiveInbox({root, scope:'https://localhost:8443|principal-a',sourceReader:process.env.ATRIUM_SPOOL_READER,...options}); t.after(()=>inbox.close());return {inbox,dir,root}; }
const native=()=>({message_id:'native-1',from_user_id:'person',item_list:[{type:1,text_item:{text:'这是私密报告，不要在 TV 展示'}},{type:2,image_item:{media:{full_url:'https://cdn.example/a'}}},{type:4,file_item:{file_name:'report.pdf',media:{full_url:'https://cdn.example/b'}}},{type:1,text_item:{text:'完整限定'},ref_msg:{message_item:{type:2,image_item:{media:{full_url:'https://cdn.example/ref'}}}}}]});
const context={account:'account',admitted:true};
test('native slots retain indices and full caption; reference is not a direct attachment',()=>{const m=normalizeWeixin(native(),context);assert.deepEqual(m.slots.map(s=>s.index),[1,2]);assert.equal(m.caption,'这是私密报告，不要在 TV 展示\n完整限定');assert.equal(m.identity.conversation,'person');assert.equal(m.identity.provider_message_id,'native-1');assert.throws(()=>normalizeWeixin({...native(),message_id:undefined},context),/identity/);});
test('Discord identity and positions come from original message',()=>{const m=normalizeDiscord({id:'123456789012345678',channel_id:'223456789012345678',author:{id:'323456789012345678'},content:'',attachments:[{id:'423456789012345678',filename:'a.pdf',url:'https://cdn.discordapp.com/a'},{id:'523456789012345678',filename:'b.pdf',url:'https://cdn.discordapp.com/b'}],referenced_message:{attachments:[{id:'evil'}]}},context);assert.equal(m.identity.provider_message_id,'123456789012345678');assert.deepEqual(m.slots.map(s=>s.index),[0,1]);});
test('capture is durable before cursor; replay preserves intent and original slot map',async t=>{const {inbox,root}=setup(t);const m=normalizeWeixin(native(),context);const [ref]=inbox.captureBatch([m]); assert.equal(inbox.describe(ref).intent,'awaiting_intent'); assert.equal(inbox.describe(ref).slots.length,2);assert.equal(inbox.captureBatch([m])[0],ref); inbox.setIntent(ref,'private',m.digest);assert.equal(inbox.captureBatch([m])[0],ref);assert.equal(inbox.describe(ref).intent,'private'); const changed=normalizeWeixin({...native(),item_list:[]},context);assert.throws(()=>inbox.captureBatch([changed]),/conflict/);assert.throws(()=>inbox.setIntent(ref,'inherit',m.digest),/conflict/);assert.ok(readFileSync(join(root,'inbox.sqlite')).length);});
test('whole batch fails atomically on capacity and rejected captures retain no content',t=>{const {inbox}=setup(t,{maxMessages:1});const m=normalizeWeixin(native(),context);assert.throws(()=>inbox.captureBatch([m,normalizeWeixin({...native(),message_id:'native-2'},context)]),/quota/);assert.equal(inbox.pending().length,0);assert.throws(()=>normalizeWeixin(native(),{...context,admitted:false}),/admission/);});
test('spool preserves verified bytes after source deletion and refuses symlinks',async t=>{const {inbox,dir}=setup(t);if(!inbox.sourceReader){inbox.sourceReader=join(dir,'reader');execFileSync('go',['build','-o',inbox.sourceReader,'./cmd/atrium-spool-read'],{cwd:fileURLToPath(new URL('../../',import.meta.url))});}const input=join(dir,'media');mkdirSync(input,{mode:0o700});const path=join(input,'report');writeFileSync(path,'original');const m=normalizeWeixin({...native(),item_list:[native().item_list[1]]},context);const [ref]=inbox.captureBatch([m]);await inbox.stageFile(ref,0,path,[input]);rmSync(path);assert.equal((await inbox.openStaged(ref,0)).toString(),'original');writeFileSync(path,'different');await assert.rejects(inbox.stageFile(ref,0,path,[input]),/conflict/);const link=join(input,'link');symlinkSync(path,link);await assert.rejects(inbox.stageFile(ref,0,link,[input]),/source/);});
test('lost Core reply recovers by frozen source lookup without repeating upload',async t=>{const {inbox}=setup(t);const m=normalizeDiscord({id:'123456789012345678',channel_id:'223456789012345678',author:{id:'323456789012345678'},content:'',attachments:[{id:'423456789012345678',filename:'a.txt',url:'https://cdn.discordapp.com/a'}]},context);const [ref]=inbox.captureBatch([m]);await inbox.stageStream(ref,0,[Buffer.from('report')]);let saved=null,uploads=0;const core={lookup:async source=>saved,upload:async(meta,b)=>{uploads++;saved={receipt_id:'01ARZ3NDEKTSV4RRFFQ69G5FAV',task_id:'01ARZ3NDEKTSV4RRFFQ69G5FAW',original_status:'saved',size_bytes:b.length,sha256:meta.expected_sha256,received_at:new Date().toISOString()};throw Error('lost');}};await inbox.reconcile(core,{force:true});assert.equal(inbox.describe(ref).slots[0].state,'submitting');await inbox.reconcile(core,{force:true});assert.equal(inbox.describe(ref).slots[0].state,'core_saved');assert.equal(uploads,1);});
test('private intent is retained on Core forbidden and missing intent never submits',async t=>{const {inbox}=setup(t);const m=normalizeWeixin(native(),context);const [ref]=inbox.captureBatch([m]);await inbox.stageStream(ref,1,[Buffer.from('secret')]);let calls=0;const core={lookup:async()=>{calls++;return null;},upload:async meta=>{assert.equal(meta.visibility_intent,'private');throw Object.assign(Error('forbidden'),{code:'archive_forbidden'});}};await inbox.reconcile(core,{force:true});assert.equal(calls,0);inbox.setIntent(ref,'private',m.digest);await inbox.reconcile(core,{force:true});assert.equal(inbox.describe(ref).intent,'private');assert.equal(inbox.describe(ref).slots[0].error,'archive_forbidden');assert.notEqual(inbox.describe(ref).slots[0].state,'core_saved');});
test('captured operational inbox resumes after cursor crash and erases raw chat after processing',t=>{const {inbox}=setup(t);const raw=native(),m=normalizeWeixin(raw,context);const [ref]=inbox.captureBatch([{...m,raw}]);assert.deepEqual(inbox.deliveries('openclaw-weixin','account')[0].raw,raw);inbox.processed(ref);assert.equal(inbox.deliveries('openclaw-weixin','account').length,0);assert.equal(inbox.db.prepare('SELECT raw FROM messages WHERE ref=?').get(ref).raw,'');assert.equal(inbox.describe(ref).slots.length,2);});
test('concurrent writes to the same frozen source cannot replace original bytes',async t=>{const {inbox}=setup(t);const m=normalizeWeixin({...native(),item_list:[native().item_list[1]]},context);const [ref]=inbox.captureBatch([m]);const results=await Promise.allSettled([inbox.stageStream(ref,0,[Buffer.from('first')]),inbox.stageStream(ref,0,[Buffer.from('other')])]);assert.equal(results.filter(r=>r.status==='fulfilled').length,1);assert.equal(results.filter(r=>r.status==='rejected'&&/conflict/.test(r.reason.message)).length,1);const b=await inbox.openStaged(ref,0);assert.ok(['first','other'].includes(b.toString()));});
test('expired operational credentials are removed while durable staged originals remain recoverable',async t=>{let now=0;const {inbox}=setup(t,{now:()=>now});const m=normalizeWeixin(native(),context);const [ref]=inbox.captureBatch([{...m,raw:native()}]);await inbox.stageStream(ref,1,[Buffer.from('saved plaintext')]);now=25*3600000;inbox.expire();assert.equal(inbox.db.prepare('SELECT raw FROM messages WHERE ref=?').get(ref).raw,'');const slots=inbox.db.prepare('SELECT idx,state,provider FROM slots WHERE ref=? ORDER BY idx').all(ref);assert.equal(slots[0].state,'staged');assert.equal(slots[1].state,'terminal');assert.equal(slots[1].provider,'{}');assert.equal((await inbox.openStaged(ref,1)).toString(),'saved plaintext');});
test('completed chat tombstones do not exhaust active capture quota',t=>{const {inbox}=setup(t,{maxMessages:1});const first=normalizeWeixin({...native(),item_list:[native().item_list[0]]},context);const [ref]=inbox.captureBatch([first]);inbox.processed(ref);const next=normalizeWeixin({...native(),message_id:'next',item_list:[native().item_list[0]]},context);assert.equal(inbox.captureBatch([next]).length,1);});

test('download window excludes staged, future and exhausted slots and advances through mixed backlog', async t => {
  let now = 1000;
  const { inbox } = setup(t, { now: () => now });
  const due = [];
  for (let i = 0; i < 136; i++) {
    const message = normalizeWeixin({ ...native(), message_id: `queue-${i}`, item_list: native().item_list.slice(0, 2) }, context);
    const [ref] = inbox.captureBatch([message]);
    if (i < 32) {
      await inbox.stageStream(ref, 1, [Buffer.from('waiting')]);
      inbox.setIntent(ref, 'needs_clarification', message.digest);
    } else if (i < 64) {
      inbox.db.prepare('UPDATE slots SET next_attempt=? WHERE ref=?').run(1000000, ref);
    } else if (i < 96) {
      inbox.db.prepare('UPDATE slots SET attempts=32 WHERE ref=?').run(ref);
    } else due.push(ref);
    now++;
  }
  assert.deepEqual(inbox.pending(), due.slice(0, 32));
  for (const ref of inbox.pending()) await inbox.stageStream(ref, 1, [Buffer.from('downloaded')]);
  assert.deepEqual(inbox.pending(), due.slice(32));
  for (const ref of inbox.pending()) await inbox.stageStream(ref, 1, [Buffer.from('downloaded')]);
  assert.deepEqual(inbox.pending(), []);
});

test('delivery limit is scoped to native channel and account before ordering and limiting', t => {
  let now = 1000;
  const { inbox } = setup(t, { now: () => now });
  const addWeixin = (account, id) => {
    const raw = { ...native(), message_id: id, item_list: [native().item_list[0]] };
    const [ref] = inbox.captureBatch([{ ...normalizeWeixin(raw, { ...context, account }), raw }]);
    now++;
    return ref;
  };
  for (let i = 0; i < 256; i++) addWeixin('account-a', `a-${i}`);
  for (let i = 0; i < 256; i++) {
    const raw = { id: String(123456789000000000n + BigInt(i)), channel_id: '223456789012345678', author: { id: '323456789012345678' }, content: 'record', attachments: [] };
    inbox.captureBatch([{ ...normalizeDiscord(raw, { ...context, account: 'account-b' }), raw }]);
    now++;
  }
  const wanted = [addWeixin('account-b', 'b-1'), addWeixin('account-b', 'b-2')];
  assert.deepEqual(inbox.deliveries('openclaw-weixin', 'account-b').map(x => x.ref), wanted);
  assert.equal(inbox.deliveries('openclaw-weixin', 'account-a').length, 256);
  assert.equal(inbox.deliveries('discord', 'account-b').length, 256);
});
