// Real native normalizers + durable inbox + TLS Core, synthetic transport only.
// No external message is sent and no attachment URL is fetched.
import assert from 'node:assert/strict';
import {readFileSync,writeFileSync,mkdirSync,mkdtempSync} from 'node:fs';
import {resolve,join} from 'node:path';
import {fileURLToPath,pathToFileURL} from 'node:url';
const [directory,fixtures]=process.argv.slice(2);
const root=resolve(fileURLToPath(new URL('../../..',import.meta.url)));
const d=resolve(directory);assert.equal(readFileSync(join(d,'SYNTHETIC-ONLY'),'utf8'),'Atrium synthetic archive acceptance v1\n');
const state=JSON.parse(readFileSync(join(d,'state.json'),'utf8')),server=JSON.parse(readFileSync(join(d,'browser-state.json'),'utf8'));
const manifest=JSON.parse(readFileSync(join(fixtures,'manifest.json'),'utf8'));assert.equal(manifest.synthetic_only,true);
const {ArchiveCore}=await import(pathToFileURL(join(root,'adapters/openclaw/archive-core.mjs')));
const {ArchiveInbox,normalizeDiscord,normalizeWeixin}=await import(pathToFileURL(join(root,'adapters/openclaw/archive-ingress.mjs')));
const tokenFile=join(d,'ingress-token');writeFileSync(tokenFile,state.token,{mode:0o600});
const core=new ArchiveCore({origin:server.origin,caFile:join(d,'ca.crt'),tokenFile,principal:state.actor.PrincipalID});
const inboxDir=mkdtempSync(join(d,'native-inbox-'));const inbox=new ArchiveInbox({root:inboxDir,scope:core.scope});
const evidence=[];
try {
 for(const id of ['H01','H03']) {
  const f=manifest.fixtures.find(x=>x.id===id),bytes=readFileSync(join(fixtures,f.file));
  const wx=normalizeWeixin({message_id:`t15-native-${id}`,from_user_id:'synthetic-person',item_list:[{type:4,file_item:{file_name:f.file,media:{full_url:'https://novac2c.cdn.weixin.qq.com/synthetic-never-fetched'}}}]},{account:'t15-synthetic',admitted:true});
  const dc=normalizeDiscord({id:id==='H01'?'523456789012345678':'623456789012345678',channel_id:'223456789012345678',author:{id:'323456789012345678'},attachments:[{id:'423456789012345678',filename:f.file,url:'https://cdn.discordapp.com/synthetic-never-fetched'}]},{account:'t15-synthetic',admitted:true});
  const refs=inbox.captureBatch([wx,dc]);for(const ref of refs) await inbox.stageStream(ref,0,[bytes]);await inbox.reconcile(core,{force:true});
  for(const ref of refs) {const slot=inbox.describe(ref).slots[0];assert.equal(slot.state,'core_saved');assert.equal(slot.receipt.original_status,'saved');evidence.push({fixture:id,ref,slot});}
  // Same channel/message replay must return the same durable records.
  assert.deepEqual(inbox.captureBatch([wx,dc]),refs);await inbox.reconcile(core,{force:true});
 }
 writeFileSync(join(d,`native-duplicate-${Date.now()}.json`),JSON.stringify({synthetic_transport:true,actual_external_delivery:false,evidence},null,2),{mode:0o600});console.log('native inbox deliveries saved; Core identity/privacy assertions still required');
}finally{inbox.close();}
