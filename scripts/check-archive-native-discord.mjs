// Executes patched installed Discord dispatch/run queue and real SDK lifecycles.
// Network, allowlist decision, and model execution are explicit test doubles.
import assert from 'node:assert/strict';
import{mkdtempSync,mkdirSync,cpSync,readdirSync,symlinkSync,readFileSync,writeFileSync,rmSync}from'node:fs';import{tmpdir}from'node:os';import{join,resolve}from'node:path';import{pathToFileURL}from'node:url';
import{transformDiscordHandler}from'./patch-archive-ingress.mjs';
import{ArchiveInbox}from'../adapters/openclaw/archive-ingress.mjs';import{ArchiveBridge,BRIDGE_SYMBOL}from'../adapters/openclaw/archive-bridge.mjs';
import{registerPlugin,TOOL_FIELDS}from'../adapters/openclaw/index.js';
const pkg=process.argv[2],source=join(pkg,'dist/.setup'),dir=mkdtempSync(join(tmpdir(),'atrium-native-discord-'));
cpSync(source,dir,{recursive:true});
mkdirSync(join(dir,'node_modules'));
for(const name of readdirSync(join(pkg,'node_modules')))if(name!=='openclaw')symlinkSync(join(pkg,'node_modules',name),join(dir,'node_modules',name));
symlinkSync('/opt/homebrew/lib/node_modules/openclaw',join(dir,'node_modules','openclaw'));
writeFileSync(join(dir,'message-handler-B-Jk4oDk.mjs'),transformDiscordHandler(readFileSync(join(source,'message-handler-B-Jk4oDk.mjs'),'utf8')));
mkdirSync(join(dir,'inbox'),{mode:0o700});let now=Date.now();const inbox=new ArchiveInbox({root:join(dir,'inbox'),scope:'synthetic',now:()=>now});
let online=false,uploads=0,modelCalls=0;const saved=new Map();
const core={lookup:async source=>saved.get(JSON.stringify(source))??null,upload:async(meta,b)=>{if(!online)throw Error('offline');uploads++;const receipt={receipt_id:'01ARZ3NDEKTSV4RRFFQ69G5FAV',task_id:'01ARZ3NDEKTSV4RRFFQ69G5FAW',sha256:meta.expected_sha256,size_bytes:b.length,original_status:'saved',received_at:new Date().toISOString()};return receipt;}};
const bridge=new ArchiveBridge({inbox,core,download:async function*(){yield Buffer.from('synthetic original');}});globalThis[BRIDGE_SYMBOL]=bridge;
const toolsFile=join(dir,'tools.json');writeFileSync(toolsFile,JSON.stringify(Object.entries(TOOL_FIELDS).map(([name,fields])=>({name,description:'fixture',inputSchema:{type:'object',additionalProperties:false,properties:Object.fromEntries(fields.map(f=>[f,{type:'string'}])),required:[]}}))),{mode:0o600});
const hooks={},claimed=new Set();const plugin=registerPlugin({pluginConfig:{agentId:'home',hostBinary:'/unused',hostArgs:[],toolsFile},on(n,f){hooks[n]=f;},registerTool(){},registerService(){}},{archiveBridge:bridge,host:{async rpc(req){assert.equal(req.method,'ingress');const first=!claimed.has(req.run_id);claimed.add(req.run_id);return{claimed:first};},close(){}}});
let handler;const events=[];
try{
 const {t:createHandler}=await import(pathToFileURL(join(dir,'message-handler-B-Jk4oDk.mjs')));
 const provider=await import(pathToFileURL(join(dir,'provider-B_G5hASF.mjs')));const map=Object.values(provider).find(v=>typeof v==='function'&&v.name==='mapGatewayDispatchData');assert.ok(map);
 handler=createHandler({accountId:'account',cfg:{},discordConfig:{},client:{},runtime:{error(message){events.push('error:'+message);}},readPolicy:async()=>({cfg:{},isCurrent:()=>true}),testing:{
 createIngressMonitor({dispatch}){return{start(){},stop(){},async accept(raw){const data=map({},'MESSAGE_CREATE',raw);return dispatch(data,{onAdopted:async()=>events.push('adopted'),onFailed:async()=>events.push('failed')});}};},
 preflightDiscordMessage:async params=>{events.push('preflight');assert.deepEqual(params.precedingMessages,[]);const ref=await bridge.captureDiscord(params.data.__atriumNative,{accountId:'account',admitted:true});events.push('captured');return{message:params.data.message,__atriumArchiveRef:ref};},
 processDiscordMessage:async ctx=>{assert.ok(bridge.current());const identity=inbox.manifest(bridge.current().ref).identity;const hookCtx={channelId:'discord',accountId:identity.account,conversationId:identity.conversation,senderId:identity.sender,messageId:identity.provider_message_id,sessionKey:'agent:home:discord:direct:synthetic'};const answer=await hooks.before_dispatch({messageId:identity.provider_message_id,channel:'discord'},hookCtx);if(!answer?.handled){modelCalls++;events.push('model');}else events.push('duplicate');},
 }});
 const raw={id:'123456789012345678',channel_id:'223456789012345678',author:{id:'323456789012345678',username:'synthetic',discriminator:'0'},content:'',timestamp:new Date().toISOString(),type:0,attachments:[{id:'423456789012345678',filename:'first.txt',url:'https://cdn.discordapp.com/a'},{id:'523456789012345678',filename:'second.txt',url:'https://cdn.discordapp.com/b'}]};
 await handler(raw);await new Promise(r=>setTimeout(r,100));assert.equal(modelCalls,1,events.join(','));assert.equal(uploads,0);online=true;now+=10000;await handler(raw);await new Promise(r=>setTimeout(r,100));assert.equal(modelCalls,1);assert.equal(uploads,2);assert.ok(events.indexOf('captured')<events.indexOf('model'));assert.ok(events.indexOf('model')<events.indexOf('adopted'));assert.ok(events.includes('duplicate'));console.log(JSON.stringify({native_dispatch:true,slots:2,consumed_turn_model_calls:modelCalls,recovered_uploads:uploads,capture_before_adoption:true}));
}finally{await handler?.deactivate();await plugin.close();delete globalThis[BRIDGE_SYMBOL];rmSync(dir,{recursive:true,force:true});}
