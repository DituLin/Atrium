import {readFileSync,writeFileSync,renameSync,existsSync,openSync,closeSync,fsyncSync} from 'node:fs';
import {join,resolve} from 'node:path';
import {createHash} from 'node:crypto';
import {fileURLToPath} from 'node:url';
const symbol='Symbol.for("io.atrium.archive-ingress.v1")';
const mark='// Atrium archive ingress v1: pinned native boundary.';
function replaceOne(s,anchor,value){if(s.split(anchor).length!==2)throw Error('archive_patch_anchor');return s.replace(anchor,value);}
export function transformWeixinProcess(s,ts){
 const suffix=`\n${mark}\nexport async function atriumArchiveAdmission(full${ts?': WeixinMessage':''}, deps${ts?': Pick<ProcessMessageDeps, "accountId" | "config" | "channelRuntime">':''}) {\n const senderId=full.from_user_id ?? "";\n if(!senderId) return false;\n const {senderAllowedForCommands}=await resolveSenderCommandAuthorizationWithRuntime({cfg:deps.config,rawBody:(full.item_list??[]).filter(i=>i.type===1).map(i=>i.text_item?.text??"").join("\\n"),isGroup:false,dmPolicy:"pairing",configuredAllowFrom:[],configuredGroupAllowFrom:[],senderId,isSenderAllowed:(id${ts?': string':''},list${ts?': string[]':''})=>list.length===0||list.includes(id),readAllowFromStore:async()=>{const entries=readFrameworkAllowFromList(deps.accountId);if(entries.length)return entries;const uid=loadWeixinAccount(deps.accountId)?.userId?.trim();return uid?[uid]:[];},runtime:deps.channelRuntime.commands});\n return !["disabled","unauthorized"].includes(resolveDirectDmAuthorizationOutcome({isGroup:false,dmPolicy:"pairing",senderAllowedForCommands}));\n}\n`;
 if(s.includes(mark))throw Error('archive_patch_already');return s+'\n'+suffix;
}
export function transformWeixinMonitor(s,ts){
 const g=ts?'(globalThis as any)':'globalThis';
 s=replaceOne(s,'import { processOneMessage }','import { processOneMessage, atriumArchiveAdmission }');
 const before=`${mark}\n      const atriumBridge=${g}[${symbol}];\n      if(!atriumBridge) throw new Error("archive_bridge_unavailable");\n      const atriumAdmit=async(full${ts?': Parameters<typeof processOneMessage>[0]':''})=>(!opts.allowFrom?.length||opts.allowFrom.includes(full.from_user_id??""))&&await atriumArchiveAdmission(full,{accountId,config,channelRuntime});\n      const atriumList=await atriumBridge.captureWeixinBatch(resp.msgs??[],{accountId,admit:atriumAdmit});\n      `;
 s=replaceOne(s,'if (resp.get_updates_buf != null && resp.get_updates_buf !== "") {',before+'if (resp.get_updates_buf != null && resp.get_updates_buf !== "") {');
 s=replaceOne(s,'const list = resp.msgs ?? [];','const list = atriumList;');
 s=replaceOne(s,'for (const full of list) {','for (const atriumDelivery of list) {\n const full=atriumDelivery.raw;\n if(!await atriumAdmit(full)){atriumBridge.rejectDelivery(atriumDelivery.ref);continue;}');
 s=replaceOne(s,'await processOneMessage(full, {','await atriumBridge.run(atriumDelivery.ref,()=>processOneMessage(full, {');
 const ending=ts?'          errLog,\n        });':'                    errLog,\n                });';
 s=replaceOne(s,ending,ending.slice(0,-3)+'}));');return s;
}
export function transformDiscordHandler(s){
 s=replaceOne(s,'\t\tshouldDebounce: (entry) => {','\t\tshouldDebounce: (entry) => {\n\t\t\tif(globalThis['+symbol+'])return false;');
 s=replaceOne(s,'\t\t\tconst entry = {\n\t\t\t\tdata,','\t\t\tconst entry = {\n\t\t\t\tdata: {...data,__atriumNative: structuredClone(data.message?.rawData ?? data)},');
 const old='\t\tawait (params.testing?.processDiscordMessage ?? (await loadMessageProcessRuntime()).processDiscordMessage)({\n\t\t\t...params.job.context,\n\t\t\tabortSignal\n\t\t});';
 s=replaceOne(s,old,`\t\t${mark}\n\t\tconst atriumProcess=async()=>await (params.testing?.processDiscordMessage ?? (await loadMessageProcessRuntime()).processDiscordMessage)({...params.job.context,abortSignal});\n\t\tif(params.job.context.__atriumArchiveRef)await globalThis[${symbol}].run(params.job.context.__atriumArchiveRef,atriumProcess);else await atriumProcess();`);return s;
}
export function transformDiscordPreflight(s){
 s=replaceOne(s,'\tconst preparedMedia = await resolveMediaList(message, params.mediaMaxBytes, mediaResolveOptions);',`\t${mark}\n\tconst atriumBridge=globalThis[${symbol}];\n\tif(!atriumBridge)throw new Error("archive_bridge_unavailable");\n\tconst atriumArchiveRef=await atriumBridge.captureDiscord(data.__atriumNative,{accountId:params.accountId,admitted:true,agentId:effectiveRoute.agentId});\n\tconst preparedMedia = await resolveMediaList(message, Math.min(params.mediaMaxBytes,32*1024*1024), mediaResolveOptions);`);
 s=replaceOne(s,'\t\tcfg: params.cfg,\n\t\tclient: params.client,\n\t\tdiscordConfig: params.discordConfig,','\t\t__atriumArchiveRef:atriumArchiveRef,\n\t\tcfg: params.cfg,\n\t\tclient: params.client,\n\t\tdiscordConfig: params.discordConfig,');return s;
}
export function transformWeixinDecrypt(_s,ts){const g=ts?'(globalThis as any)':'globalThis';return `${mark}\nexport async function downloadAndDecryptBuffer(encryptedQueryParam${ts?':string':''},aesKeyBase64${ts?':string':''},cdnBaseUrl${ts?':string':''},label${ts?':string':''},fullUrl${ts?'?:string':''}) {const bridge=${g}[${symbol}];if(!bridge)throw new Error("archive_bridge_unavailable");return bridge.downloadBuffer({kind:"weixin",media:{encrypt_query_param:encryptedQueryParam,aes_key:aesKeyBase64,full_url:fullUrl}});}\nexport async function downloadPlainCdnBuffer(encryptedQueryParam${ts?':string':''},cdnBaseUrl${ts?':string':''},label${ts?':string':''},fullUrl${ts?'?:string':''}){const bridge=${g}[${symbol}];if(!bridge)throw new Error("archive_bridge_unavailable");return bridge.downloadBuffer({kind:"weixin",media:{encrypt_query_param:encryptedQueryParam,full_url:fullUrl}});}\n`;}
export function transformWeixinMedia(s){
 const pattern=/logger\.debug\(\s*`\$\{label\} image: encrypt_query_param=[\s\S]*?`\s*,?\s*\);/g;
 const matches=s.match(pattern);if(matches?.length!==1)throw Error('archive_patch_anchor');
 return s.replace(pattern,'logger.debug(`${label} image: bounded archive media download`);').replace('const WEIXIN_MEDIA_MAX_BYTES = 100 * 1024 * 1024;','const WEIXIN_MEDIA_MAX_BYTES = 32 * 1024 * 1024;');
}
const sha=s=>createHash('sha256').update(s).digest('hex');
export function buildPlan({weixin,discord}){
 const pins=JSON.parse(readFileSync(new URL('./archive-ingress-pins.json',import.meta.url)));
 const transforms={weixin_monitor:transformWeixinMonitor,weixin_process:transformWeixinProcess,weixin_decrypt:transformWeixinDecrypt,weixin_media:transformWeixinMedia,discord_handler:transformDiscordHandler,discord_preflight:transformDiscordPreflight};
 for(const [kind,root]of Object.entries({weixin,discord})){if(JSON.parse(readFileSync(join(root,'package.json'))).version!==pins[kind].version)throw Error('archive_patch_version');}
 return pins.files.map(pin=>{const path=join(pin.kind==='weixin'?weixin:discord,pin.path);const original=readFileSync(path,'utf8');if(sha(original)===pin.patched_sha256)return{path,original,patched:original,changed:false};if(sha(original)!==pin.sha256)throw Error('archive_patch_hash');const patched=transforms[pin.transform](original,pin.path.endsWith('.ts'));if(sha(patched)!==pin.patched_sha256)throw Error('archive_patch_transform');return{path,original,patched,changed:true};});
}
export function applyPlan(plan){for(const f of plan){if(!f.changed)continue;const backup=f.path+'.atrium-archive-original';if(existsSync(backup)&&readFileSync(backup,'utf8')!==f.original)throw Error('archive_patch_backup');}
 for(const f of plan){if(!f.changed)continue;const backup=f.path+'.atrium-archive-original';if(!existsSync(backup))writeFileSync(backup,f.original,{flag:'wx',mode:0o600});const temp=f.path+'.atrium-archive-next';writeFileSync(temp,f.patched,{flag:'wx',mode:0o600});const fd=openSync(temp,'r');try{fsyncSync(fd);}finally{closeSync(fd);}renameSync(temp,f.path);}}
if(process.argv[1]&&resolve(process.argv[1])===fileURLToPath(import.meta.url)){try{const args=process.argv.slice(2);if(args.length!==3||!['--check','--apply'].includes(args[0]))throw Error('usage: --check|--apply WEIXIN_PACKAGE DISCORD_PACKAGE');const plan=buildPlan({weixin:resolve(args[1]),discord:resolve(args[2])});if(args[0]==='--apply')applyPlan(plan);console.log(JSON.stringify({verified:plan.length,changed:plan.filter(x=>x.changed).length,applied:args[0]==='--apply'}));}catch(e){console.error(e.message);process.exitCode=1;}}
