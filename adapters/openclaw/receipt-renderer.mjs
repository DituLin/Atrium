const fields={home_show_archive_query:["screen_id","query_ref"],home_accept_archive_input:["visibility_intent","input_kind"],"home_analyze_archives":["operation", "count", "metric", "query", "entity_id", "domain", "document_type", "date_from", "date_to"],"home_search_archives": ["query", "entity_id", "domain", "document_type", "date_from", "date_to", "ownership", "limit", "cursor"], "home_get_archive_evidence": ["document_id", "limit", "cursor"], "home_search_archive_entities": ["query", "entity_id", "limit", "cursor"], "home_get_archive_task": ["task_id"], "home_get_archive_question": ["task_id"], "home_answer_archive_question": ["task_id", "question_id", "question_version", "waiting_task_version", "extraction_id", "document_revision", "action", "target", "name"], "home_revise_archive": ["document_id", "document_revision", "kind", "reason", "observation_id", "value", "entity_id"],home_list_family_members: [], home_list_family_records: ['member_id','limit','cursor'], home_record_family_entry: ['member_id','category','occurred_at','title','notes','measurements','display_on_tv','supersedes_id','revision_reason','retracted'],home_get_status:[],home_list_screens:[],home_get_screen:['screen_id'],home_get_nas_status:[],home_list_photos:['collection','limit','cursor'],home_get_photo:['photo_id'],home_get_command:['command_id'],home_navigate_screen:['screen_id','route','collection'],home_show_photo:['screen_id','photo_id'],home_refresh_screen:['screen_id']};
const object=v=>v!==null&&typeof v==='object'&&!Array.isArray(v);
const ulid=v=>typeof v==='string'&&/^[0-7][0-9A-HJKMNP-TV-Z]{25}$/.test(v);
const id=v=>typeof v==='string'&&/^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$/.test(v)?v:'未知';
const stamp=v=>typeof v==='string'&&/^\d{4}-\d{2}-\d{2}T.*(?:Z|[+-]\d{2}:\d{2})$/.test(v)&&Number.isFinite(Date.parse(v))?v:null;
const fixed=(v,allowed)=>allowed.includes(v)?v:'unknown';
const actionTools=new Set(['home_show_archive_query','home_navigate_screen','home_show_photo','home_refresh_screen']);
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
  if(r.tool==='home_accept_archive_input'&&!validArchiveInput(r.tool_result?.structuredContent?.data,r.arguments))throw new Error('invalid_archive_input_receipt');
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
const safeText=v=>typeof v==='string'?v.replace(/[\x00-\x1f\x7f]/g,' ').slice(0,16000):'';
function familyRecord(r){
 const status=r?.retracted===true?'已撤回，历史内容':r?.superseded_by?'已被更正，历史内容':r?.supersedes_id?'当前更正记录':'记录';
 return `${status} ${id(r?.id)} · 成员 ${id(r?.member_id)} · ${safeText(r?.title)} · ${stamp(r?.occurred_at)??'时间未知'}${Array.isArray(r?.measurements)?r.measurements.filter(m=>Number.isFinite(m.value)).map(m=>`；${safeText(m.metric)} ${m.value} ${safeText(m.unit)}`).join(''):''}${r?.notes?'；'+safeText(r.notes):''}${r?.revision_reason?'；原因：'+safeText(r.revision_reason):''}${r?.superseded_by?'；后续记录 '+id(r.superseded_by):''}`;
}
const boundary='当前支持家庭状态、NAS、屏幕和照片查询，以及指定屏幕的页面切换、照片展示和刷新。视频、人物搜索与日历尚未接入。';
function actionLabel(c){
 const p=c?.payload;
 if(c?.kind==='refresh')return '刷新屏幕';
 if(c?.kind==='archive'&&id(p?.archive_view_id)!=='未知')return '展示当前公开档案查询结果';
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
  if(r.error?.code==='tool_budget'){lines.push('已达到本轮 8 次工具调用上限，本轮未完成；以上只包含已取得的结果。');continue;}
  if(r.error||r.tool_result?.isError||!object(env)||env.schema_version!=='1'||!stamp(env.observed_at)){lines.push(`${r.tool}：未取得可核实结果。`);continue;}
  const d=env.data,when=`观察时间 ${env.observed_at}；availability=${fixed(env.availability,['available','unavailable','unknown','partial'])}。`;
  switch(r.tool){
   case 'home_accept_archive_input':lines.push(renderArchiveInput(d));break;
   case 'home_analyze_archives':lines.push(...analysisLines(r,d,index));break;
   case 'home_search_archives':case 'home_get_archive_evidence':case 'home_search_archive_entities':case 'home_get_archive_task':case 'home_get_archive_question':case 'home_answer_archive_question':case 'home_revise_archive':lines.push(...archiveLines(r,d,index));break;
   case 'home_list_family_members':lines.push(...(Array.isArray(d?.members)?d.members.map(m=>`家庭成员：${safeText(m.name)}（${id(m.id)}）。`):['成员查询未返回有效数据。']));break;
   case 'home_list_family_records':lines.push(`家庭记录：本页 ${Array.isArray(d?.records)?d.records.length:0} 条，不代表全部历史。`,...(Array.isArray(d?.records)?d.records.map(familyRecord):[]));break;
   case 'home_record_family_entry':
    if(!object(d?.record)||!ulid(d.record.id)||!['pending','synced'].includes(d.nas_status)){lines.push('家庭记录：无法确认保存结果。');break;}
    lines.push(`${d.record.retracted===true?'已保存撤回记录':d.record.supersedes_id?'已保存更正记录':'已记录'}：${familyRecord(d.record)}。NAS ${d.nas_status==='synced'?'已同步':'待同步'}。${d.replayed?'本次为原记录回执，未重复新增。':''}`);break;
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
 if(actions.size===0&&!receipts.some(r=>r.tool.startsWith('home_list_family_')||r.tool==='home_record_family_entry'||r.tool==='home_accept_archive_input'||Object.hasOwn(archiveTools,r.tool)))lines.push(boundary+'本轮回执没有屏幕动作；如需控制，请指定目标与动作。');
 if(failed||timedOut)lines.push('本轮未完整取得可核实结果；以上仅为已取得的可信回执。可能还有未取得回执的动作，请使用宿主 --recover 查询账本，不要重发。');
 return lines.join('\n');
}

const archiveTools={home_analyze_archives:1,home_search_archives:1,home_get_archive_evidence:1,home_search_archive_entities:1,home_get_archive_task:1,home_get_archive_question:1,home_answer_archive_question:1,home_revise_archive:1};
function archiveLines(r,d,receiptIndex=0){
 if(!object(d)||d.schema_version!=='archive-query/1'||r.tool_result?.structuredContent?.availability!=='available')return ['档案：未取得可核实结果。'];
 const lines=[],x=d.result;
 if(r.tool==='home_revise_archive'){
  if(!Number.isSafeInteger(r.arguments.document_revision)||x?.document_revision!==r.arguments.document_revision+1)return ['档案：无法确认更正结果，请查询当前版本。'];
  return [`已更正档案 ${safeText(r.arguments.document_id)}，Core 确认版本 ${x.document_revision}；更正类型 ${{value:"数值更正",reassign:"调整归属",private:"设为私密",withdraw:"撤回档案"}[r.arguments.kind]??"未知"}。原件和历史保留，NAS 同步状态未确认。`];
 }
 if(r.tool==='home_get_archive_task'||r.tool==='home_answer_archive_question'){
  if(x?.id!==r.arguments.task_id||!['queued','running','retry_wait','waiting_input','succeeded','failed','cancelled'].includes(x?.status))return ['档案任务：无法确认结果。'];
  return [`${r.tool==='home_answer_archive_question'?'Core 已记录本次澄清；':''}档案任务（${safeText(x.id).slice(-8)}）：${{queued:"排队中",running:"处理中",retry_wait:"等待重试",waiting_input:"等待确认",succeeded:"处理完成",failed:"处理失败",cancelled:"已取消"}[x.status]}；原件 ${x.original_status==="saved"?"已保存":"状态未确认"}，NAS ${x.nas_status==="synced"?"已同步":"待同步"}。${x.status==='queued'||x.status==='running'?'仍在处理，尚未确认发布完成。':''}${x.result?.parse_status?'解析状态 '+safeText(x.result.parse_status)+'。':''}`];
 }
 if(r.tool==='home_get_archive_question'){
  if(!x?.question_id||!Number.isSafeInteger(x.question_version)||!Array.isArray(x.actions))return ['档案澄清：无法确认问题。'];
  lines.push(`需要确认：${safeText(x.subject_name)||"这份档案的归属或内容"}。`);
  for(const [target,label]of Object.entries(x.target_labels??{}))lines.push(`候选 ${safeText(label)}（${safeText(target).slice(-8)}）。`);
  lines.push(`允许操作：${x.actions.map(a=>({keep_current:'保留当前状态',create_subject:'建立新档案主体',select_subject:'选择现有主体'}[a]??'未知操作')).join('、')}。${x.targets_truncated?'候选不完整，请继续查询。':''}`);return lines;
 }
 if(!Array.isArray(d.items)||!object(d.coverage)||typeof d.coverage.complete!=='boolean')return ['档案：未取得完整的查询回执。'];
 lines.push(`档案查询：本页 ${d.items.length} 项，仅代表本次授权检索范围。`);
 if(r.tool==='home_search_archive_entities'){
  for(const item of d.items)lines.push(`档案主体 ${safeText(item.id)}：${Array.isArray(item.names)?item.names.map(safeText).join('、'):''}${Array.isArray(item.aliases)&&item.aliases.length?'；别名 '+item.aliases.map(safeText).join('、'):''}`);
 }else{
  for(const [itemIndex,item] of d.items.entries()){
   const domain={health:'健康',education:'教育',finance:'财务',property:'房屋',other:'其他',unknown:'待分类'}[item.domain]??safeText(item.domain);
   const kind={checkup:'体检报告',report_card:'成绩报告',receipt:'收据',bill:'账单',warranty:'保修资料',repair:'维修记录'}[item.document_type]??safeText(item.document_type);
   const owner={confirmed:'归属已确认',household:'全家共有',waiting:'归属待确认',unassigned:'尚未归属'}[item.ownership]??'';
   lines.push(`${domain}档案 · ${kind}（${safeText(item.document_id).slice(-8)}）${owner?' · '+owner:''}。${item.review_required?'需要澄清，暂不用于完整统计。':''}${item.metadata_only?'索引尚未就绪，仅返回档案信息。':''}`);
   for(const [evidenceIndex,ev] of (item.evidence??[]).entries()){
    const ref=`${receiptIndex+1}.${itemIndex+1}.${evidenceIndex+1}`;
    const citation=`[来源 ${ref} · ${archiveLocation(ev)}]`;
    const label=ev.source_kind==='original'?'原文摘录':ev.confirmed?'已确认提取':'待确认候选';
    lines.push(`${label}${ev.field_name?" · "+safeText(ev.field_name):""}：${safeText(ev.excerpt)} ${citation}`);
    if(ev.assertion_id)lines.push(`人工更正：${safeText(ev.asserted_value)}（来源 ${ref}）；原文保留上述数值。`);
   }
  }
 }
 if(!d.coverage.complete)lines.push(`覆盖不完整：${safeText(d.coverage.reason)}；不能据此断言家中没有相关资料。`);
 if(d.next_cursor)lines.push('还有后续页面，本页不是全部结果。');
 return lines;
}

function archiveLocation(ev){
 const l=ev.locator??{};
 if(l.kind==='text'&&object(l.text))return `文本第 ${l.text.start+ev.start+1}–${l.text.start+ev.end} 字`;
 if(l.kind==='csv'&&object(l.csv))return `表格第 ${l.csv.record} 行、第 ${l.csv.column} 列`;
 if(l.kind==='xlsx'&&object(l.xlsx))return `${safeText(l.xlsx.sheet_name)} ${safeText(l.xlsx.cell)}`;
 if(l.kind==='docx'&&object(l.docx))return l.docx.row>0?`Word 表格第 ${l.docx.row} 行、第 ${l.docx.grid_column} 列，段落 ${l.docx.paragraph}`:`Word 第 ${l.docx.paragraph} 段`;
 if(object(l.page))return `第 ${l.page.page??l.page.page_number??'?'} 页，摘录第 ${ev.start+1}–${ev.end} 字`;
 return `摘录第 ${ev.start+1}–${ev.end} 字`;
}

function analysisLines(r,d,index){
 if(!object(d)||d.schema_version!=='archive-analysis/1'||d.algorithm_version!=='archive-analysis/5'||d.query?.operation!==r.arguments.operation||d.coverage?.scope!=='archived_only'||!Array.isArray(d.groups)||d.groups.length>256)return ['档案分析：未取得可核实结果。'];
 const lines=['档案分析：仅代表已归档且当前可见的资料。'];
 for(const [i,g] of d.groups.entries()){
  if(typeof g.value!=='string'||!/^[-+]?[0-9]+(?:\.[0-9]+)?$/.test(g.value))return ['档案分析：数值回执无效。'];
  lines.push(`${safeText(g.metric)}：${g.value} ${safeText(g.currency)||safeText(g.unit)}${g.date?' · '+safeText(g.date)+'（'+safeText(g.date_precision)+'）':''}${g.change_percent_points?'；较 '+safeText(g.baseline_date)+' 变化 '+safeText(g.change_percent_points)+' 个百分点':''}${g.denominator?'；分子 '+safeText(g.numerator)+' / 分母 '+safeText(g.denominator)+'；舍入 '+safeText(g.rounding):''}。`);
  for(const [j,ev]of(g.evidence??[]).entries())lines.push(`${safeText(ev.excerpt)}${ev.assertion_id?'；人工更正 '+safeText(ev.asserted_value):''} [来源 ${index+1}.${i+1}.${j+1} · ${safeText(ev.document_id)} · ${archiveLocation(ev)}]`);
 }
 if(!d.coverage.complete)lines.push('覆盖不完整：'+(d.coverage.reasons??[]).map(safeText).join('、')+'。不能据此推断全家总量。');
 if(d.coverage.excluded)lines.push(`排除 ${d.coverage.excluded} 项不适合本次计算的字段。`);
 return lines;
}

// Host-only intake receipts; no document read permission is implied.
export function renderArchiveInput(result) {
 if(result?.schema_version!=='archive-input/1'||!Array.isArray(result.slots)||result.slots.length>33)return '未取得可核实的档案接收结果。';
 const saved=result.slots.filter(s=>s.state==='core_saved'&&s.receipt?.original_status==='saved');
 const lines=[`档案原件已保存 ${saved.length}/${result.slots.length}。${result.intent==='private'?'本次按私密意图接收。':''}`];
 if(result.intent==='needs_clarification')lines.push('展示范围尚待澄清，未按默认展示接收。');
 for(const s of result.slots){const name=safeText(s.name);lines.push(s.state==='core_saved'&&s.receipt?.original_status==='saved'?`${name}：已保存；任务 ${id(s.receipt.task_id)}。`:`${name}：${s.state==='terminal'?'接收未完成':'仅暂存或等待接收'}，尚未保存到 Atrium。`);}
 lines.push('NAS 归档状态未核实；原件保存不代表分析完成。');return lines.join('\n');
}

function validArchiveInput(r,args){
 if(!object(r)||Object.keys(r).some(k=>!['schema_version','intent','all_saved','slots'].includes(k))||r.schema_version!=='archive-input/1'||!['inherit','private','needs_clarification'].includes(r.intent)||!Array.isArray(r.slots)||r.slots.length>33||typeof r.all_saved!=='boolean')return false;
 if(args&&(!['files','text'].includes(args.input_kind)||args.visibility_intent!==r.intent))return false;
 const indices=new Set();
 for(const s of r.slots){
  if(!object(s)||Object.keys(s).some(k=>!['slot_index','name','mime','state','sha256','size','error','receipt'].includes(k))||!Number.isSafeInteger(s.slot_index)||s.slot_index<0||s.slot_index>1023||indices.has(s.slot_index)||typeof s.name!=='string'||Buffer.byteLength(s.name)>512||typeof s.mime!=='string'||s.mime.length>255||!['downloading','staged','submitting','core_saved','terminal'].includes(s.state))return false;
  indices.add(s.slot_index);
  if(s.sha256!==null&&!(typeof s.sha256==='string'&&/^[a-f0-9]{64}$/.test(s.sha256)))return false;
  if(s.size!==null&&(!Number.isSafeInteger(s.size)||s.size<1||s.size>33554432))return false;
  if(s.error!==null&&!(typeof s.error==='string'&&/^archive_[a-z_]+$/.test(s.error)))return false;
  if(s.state==='core_saved'){
   const p=s.receipt;if(!object(p)||Object.keys(p).some(k=>!['receipt_id','task_id','original_status','sha256','size_bytes','received_at'].includes(k))||!ulid(p.receipt_id)||!ulid(p.task_id)||p.original_status!=='saved'||!stamp(p.received_at)||p.sha256!==s.sha256||p.size_bytes!==s.size)return false;
  }else if(s.receipt!==null)return false;
 }
 return r.all_saved===(r.slots.length>0&&r.slots.every(s=>s.state==='core_saved'));
}
