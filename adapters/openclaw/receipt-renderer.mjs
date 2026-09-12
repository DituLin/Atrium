const fields={home_get_status:[],home_list_screens:[],home_get_screen:['screen_id'],home_get_nas_status:[],home_list_photos:['collection','limit','cursor'],home_get_photo:['photo_id'],home_get_command:['command_id'],home_navigate_screen:['screen_id','route','collection'],home_show_photo:['screen_id','photo_id'],home_refresh_screen:['screen_id']};
const object=v=>v!==null&&typeof v==='object'&&!Array.isArray(v);
const ulid=v=>typeof v==='string'&&/^[0-7][0-9A-HJKMNP-TV-Z]{25}$/.test(v);
const id=v=>typeof v==='string'&&/^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$/.test(v)?v:'未知';
const stamp=v=>typeof v==='string'&&/^\d{4}-\d{2}-\d{2}T.*(?:Z|[+-]\d{2}:\d{2})$/.test(v)&&Number.isFinite(Date.parse(v))?v:null;
const fixed=(v,allowed)=>allowed.includes(v)?v:'unknown';
const actionTools=new Set(['home_navigate_screen','home_show_photo','home_refresh_screen']);
export function validateReceipts(raw,invocation){
 if(Buffer.byteLength(raw)>512*1024)throw new Error('invalid_receipt_size');
 const lines=raw.split('\n');if(lines.at(-1)==='')lines.pop();
 if(lines.length>8)throw new Error('invalid_receipt_count');
 return lines.map(line=>{
  if(Buffer.byteLength(line)>64*1024)throw new Error('invalid_receipt_size');
  let r;try{r=JSON.parse(line);}catch{throw new Error('invalid_receipt_json');}
  if(!object(r)||Object.keys(r).some(k=>!['schema_version','invocation_id','run_id','tool','arguments','tool_result','report','error'].includes(k))||r.schema_version!==1||r.invocation_id!==invocation||!/^([0-9a-f]{64})$/.test(r.run_id)||!Object.hasOwn(fields,r.tool)||!object(r.arguments)||Object.keys(r.arguments).some(k=>!fields[r.tool].includes(k)))throw new Error('invalid_receipt');
  if(r.tool_result!==null&&(!object(r.tool_result)||Object.keys(r.tool_result).some(k=>!['structuredContent','isError'].includes(k))||(r.tool_result.isError!==undefined&&typeof r.tool_result.isError!=='boolean')||!object(r.tool_result.structuredContent)))throw new Error('invalid_receipt_result');
  if(r.report!==undefined&&(!object(r.report)||Object.keys(r.report).some(k=>!['Status','TextZH','ScreenID','CommandID','OperationID','ObservedAt'].includes(k))))throw new Error('invalid_receipt_report');
  if(r.error!==undefined&&(!object(r.error)||Object.keys(r.error).some(k=>!['code','operation_id','command_id'].includes(k))||typeof r.error.code!=='string'))throw new Error('invalid_receipt_error');
  return r;
 });
}
export function readReceiptPrefix(raw,invocation){
 const receipts=[];let invalid=raw.length>0&&!raw.endsWith('\n');
 const lines=raw.split('\n');lines.pop(); // Only newline-committed records are evidence.
 let bytes=0;
 for(const line of lines){
  bytes+=Buffer.byteLength(line)+1;
  if(receipts.length>=8||bytes>512*1024){invalid=true;break;}
  try{receipts.push(...validateReceipts(line+'\n',invocation));}catch{invalid=true;break;}
 }
 return {receipts,invalid};
}
const boundary='当前支持家庭状态、NAS、屏幕和照片查询，以及指定屏幕的页面切换、照片展示和刷新。视频、人物搜索与日历尚未接入。';
function actionLabel(c){
 const p=c?.payload;
 if(c?.kind==='refresh')return '刷新屏幕';
 if(c?.kind==='show'&&id(p?.photo_id)!=='未知')return '展示照片 '+p.photo_id;
 if(c?.kind==='navigate'&&p?.route==='dashboard')return '切换到家庭首页';
 if(c?.kind==='navigate'&&p?.route==='photos'&&['recent','captured_today','random','all'].includes(p?.collection))return '切换到照片集合 '+p.collection;
 return '屏幕动作';
}
function screens(data){return Array.isArray(data)?data.map(s=>`屏幕 ${id(s?.id)}：应用会话${s?.online===true?'在线':s?.online===false?'离线':'状态未知'}${stamp(s?.last_seen_at)?'；最后上报 '+s.last_seen_at:''}。`):[];}
function nas(data){return Array.isArray(data)?data.map(s=>`来源 ${id(s?.id)}：health=${fixed(s?.health,['online','offline','degraded','unknown'])}${stamp(s?.last_check_at)?'；检查时间 '+s.last_check_at:''}。`):[];}
function action(r,index){
 const report=r.report,env=r.tool_result?.structuredContent,c=env?.data;
 if(!actionTools.has(r.tool)&&!(r.tool==='home_get_command'&&report))return null;
 const command=ulid(report?.CommandID)?report.CommandID:ulid(c?.id)?c.id:ulid(r.error?.command_id)?r.error.command_id:null;
 const operation=ulid(report?.OperationID)?report.OperationID:ulid(r.error?.operation_id)?r.error.operation_id:null;
 const screen=id(r.arguments.screen_id??report?.ScreenID??c?.screen_id);
 const matches=env?.schema_version==='1'&&object(c)&&report?.ScreenID===c.screen_id&&(!r.arguments.screen_id||r.arguments.screen_id===c.screen_id)&&(!r.arguments.command_id||r.arguments.command_id===c.id)&&stamp(report?.ObservedAt)&&Date.parse(report.ObservedAt)===Date.parse(env?.observed_at);
 const valid=matches&&!r.error&&!r.tool_result?.isError&&object(c)&&ulid(c.id)&&c.id===command&&['accepted','applied'].includes(c.status)&&report?.Status===c.status&&report.CommandID===c.id&&stamp(env.observed_at);
 // A Core failed/expired/unknown command is a valid negative result, never success.
 const negative=matches&&(!r.error||r.error.code==='tool_error')&&r.tool_result?.isError===true&&object(c)&&c.id===command&&['failed','expired','unknown'].includes(c.status)&&report?.Status===c.status&&stamp(env.observed_at);
 const status=valid||negative?c.status:'unconfirmed';
 const when=valid||negative?env.observed_at:null;
 const labels={accepted:'命令已接受，尚未确认执行',applied:'已在该观察时刻确认执行',failed:'命令执行失败',expired:'命令已过期，未确认执行',unknown:'执行结果未知',unconfirmed:'未取得可核实的执行结果'};
 return {key:command??operation??`receipt-${index}`,status,when,text:`屏幕 ${screen}：${actionLabel(c)}；${labels[status]}。${when?' 观察时间 '+when+'。':''}${operation?' 操作 '+operation+'。':''}${command?' 命令 '+command+'。':''}`};
}
export function renderReceipts(receipts,{failed=false,timedOut=false}={}){
 if(receipts.length===0)return failed||timedOut?'AI 本轮暂不可用：未取得可核实结果，无法确认是否已执行动作；请使用宿主 --recover 查询账本，不要自动重发。':'本轮没有工具执行回执。'+boundary+'请明确所需操作。';
 const lines=[],actions=new Map();let candidates=0;
 for(const [index,r] of receipts.entries()){
  const a=action(r,index);if(a){const prior=actions.get(a.key);if(prior&&['applied','failed','expired','unknown'].includes(prior.status)&&a.status!==prior.status){prior.conflict=true;}else if(!prior||!prior.when||!a.when||Date.parse(a.when)>=Date.parse(prior.when)){actions.set(a.key,a);}continue;}
  const env=r.tool_result?.structuredContent;
  if(r.error||r.tool_result?.isError||!object(env)||env.schema_version!=='1'||!stamp(env.observed_at)){lines.push(`${r.tool}：未取得可核实结果。`);continue;}
  const d=env.data,when=`观察时间 ${env.observed_at}；availability=${fixed(env.availability,['available','unavailable','unknown','partial'])}。`;
  switch(r.tool){
   case 'home_get_status':lines.push(`家庭中枢：core=${fixed(d?.core,['reachable','unreachable','unknown'])}。${when}`,...nas(d?.nas),...screens(d?.screens));break;
   case 'home_get_nas_status':lines.push(when,...nas(d));break;
   case 'home_list_screens':candidates=Math.max(candidates,Array.isArray(d)?d.length:0);lines.push(when,...screens(d));break;
   case 'home_get_screen':lines.push(when,...screens(object(d)?[d]:[]));break;
   case 'home_list_photos':if(!Array.isArray(d?.items)){lines.push('照片查询没有可核实的页面数据。');break;}lines.push(`照片集合 ${fixed(d.collection,['recent','captured_today','random','all'])}：本页返回 ${d.items.length} 项，不代表总量。${when}${d.baseline_only===true?(d.items.length===0?' 暂无非基线最近新增照片；这不代表今天拍摄数量。':' 结果仅含基线照片；基线不等于今天新增。'):''}`);break;
   case 'home_get_photo':lines.push(`照片 ${id(d?.id)}：预览状态 ${fixed(d?.preview_status,['pending','ready','failed','unsupported','evicted'])}。${when}${stamp(d?.captured_at)?' 拍摄时间 '+d.captured_at+'。':''}`);break;
   case 'home_get_command':lines.push(`命令 ${id(d?.id)}：status=${fixed(d?.status,['accepted','applied','failed','expired','unknown'])}。${when}`);break;
  }
 }
 lines.push(...[...actions.values()].map(a=>a.text+(a.conflict?' 后续回执与该终态不一致，未能核实新证据；保留上述原观察结论。':'')));
 if(candidates>1&&actions.size===0)lines.push('有多个可选屏幕，请指定目标屏幕。');
 if(actions.size===0)lines.push(boundary+'本轮回执没有屏幕动作；如需控制，请指定目标与动作。');
 if(failed||timedOut)lines.push('本轮未完整取得可核实结果；以上仅为已取得的可信回执。可能还有未取得回执的动作，请使用宿主 --recover 查询账本，不要重发。');
 return lines.join('\n');
}
