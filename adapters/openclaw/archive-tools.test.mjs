import {readFileSync} from 'node:fs';
import test from 'node:test';
import assert from 'node:assert/strict';
import { renderReceipts,validateReceipts } from './receipt-renderer.mjs';
const invocation='aa';
function receipt(tool,data,args={}){return {schema_version:1,invocation_id:invocation,run_id:'a'.repeat(64),tool,arguments:args,tool_result:{structuredContent:{schema_version:'1',observed_at:'2026-10-11T00:00:00Z',availability:'available',data}}};}
test('archives render trusted sources and incomplete coverage without invented totals',()=>{
 const r=receipt('home_search_archives',{schema_version:'archive-query/1',items:[{document_id:'doc1',document_revision:2,domain:'health',document_type:'体检',ownership:'confirmed',parse_status:'parsed',review_required:false,entity_ids:[],dates:[],metadata_only:false,truncated:false,evidence:[{document_id:'doc1',publication_id:'pub1',publication_revision:2,source_extraction_id:'x1',segment_id:'seg1',locator:{page:2},start:1,end:8,excerpt:'血红蛋白 142',confirmed:true,asserted_value:'145',assertion_id:'audit1'}]}],coverage:{complete:false,reason:'partial'},index_status:'ready'});
 const rows=validateReceipts(JSON.stringify(r)+'\n',invocation);const text=renderReceipts(rows);
 assert.match(text,/142/);assert.match(text,/145/);assert.match(text,/doc1/);assert.match(text,/来源 1.1.1/);assert.match(text,/已确认提取/);assert.match(text,/不完整/);assert.doesNotMatch(text,/本轮回执没有屏幕动作/);
});
test('empty incomplete archive search is not absence',()=>{
 const text=renderReceipts([receipt('home_search_archives',{schema_version:'archive-query/1',items:[],coverage:{complete:false,reason:'indexing'},index_status:'indexing'})]);
 assert.match(text,/不能.*没有/);
});
test('revision must carry matching Core revision receipt',()=>{
 const r=receipt('home_revise_archive',{schema_version:'archive-query/1',result:{document_revision:5}},{document_id:'doc1',document_revision:4,kind:'value',value:'80',reason:'更正',observation_id:'obs1'});
 assert.match(renderReceipts([r]),/已更正/);
 r.tool_result.structuredContent.data.result.document_revision=99;
 assert.doesNotMatch(renderReceipts([r]),/已更正/);
});
test('original context is not an unresolved extraction and locators are human readable',()=>{
 const r=receipt('home_search_archives',{schema_version:'archive-query/1',items:[{document_id:'doc1',document_revision:2,domain:'education',document_type:'report_card',evidence:[{document_id:'doc1',source_extraction_id:'x1',segment_id:'a'.repeat(64),locator:{kind:'xlsx',xlsx:{sheet_name:'成绩表',cell:'B2'}},start:0,end:2,excerpt:'88',confirmed:false,source_kind:'original'}]}],coverage:{complete:true},index_status:'ready'});
 const text=renderReceipts([r]);assert.match(text,/原文摘录/);assert.match(text,/成绩表.*B2/);assert.doesNotMatch(text,/待确认|a{64}|"kind"/);
});
test('tool budget exhaustion explicitly leaves archive turn incomplete',()=>{
 const r={...receipt('home_search_archives',null),tool_result:null,error:{code:'tool_budget'}};
 assert.match(renderReceipts([r]),/8 次.*未完成/);
});
test('typed clarification presents family choices without implementation fields',()=>{
 const r=receipt('home_get_archive_question',{schema_version:'archive-query/1',result:{question_id:'q1',question_version:3,subject_name:'妈妈',actions:['keep_current','create_subject'],target_labels:{}}},{task_id:'task1'});
 const text=renderReceipts([r]);assert.match(text,/妈妈/);assert.match(text,/建立新档案主体/);assert.doesNotMatch(text,/create_subject|question_version/);
});
test('analysis renders exact Core totals, denominator and archived coverage',()=>{
 const data={schema_version:'archive-analysis/1',algorithm_version:'archive-analysis/6',snapshot_id:'s1',query:{operation:'ratio',metric:'score'},projection:'authorized',groups:[{metric:'score',value:'88.00',unit:'percent',numerator:'132',denominator:'150',rounding:'half_away_from_zero/2',evidence:[{document_id:'d1',segment_id:'seg1',excerpt:'132',start:0,end:3,locator:{kind:'text',text:{start:0}}}]}],coverage:{scope:'archived_only',complete:false,reasons:['partial_or_pending_sources'],included:1,excluded:1}};
 const r=receipt('home_analyze_archives',data,{operation:'ratio',metric:'score'});
 const rows=validateReceipts(JSON.stringify(r)+'\n',invocation);const text=renderReceipts(rows);
 assert.match(text,/88.00/);assert.match(text,/132.*150/);assert.match(text,/已归档/);assert.match(text,/不完整/);assert.match(text,/来源/);
});

test('retained actual analysis/6 receipts render numbers and reject unknown algorithms',()=>{
 const rows=JSON.parse(readFileSync(new URL('./testdata/t15-analysis-6-receipts.json',import.meta.url),'utf8'));
 const verified=validateReceipts(rows.map(r=>JSON.stringify(r)).join('\n')+'\n',rows[0].invocation_id);
 const text=renderReceipts(verified);assert.match(text,/140/);assert.match(text,/来源/);assert.doesNotMatch(text,/未取得可核实结果/);
 for(const r of verified)r.tool_result.structuredContent.data.algorithm_version='archive-analysis/unknown';
 assert.match(renderReceipts(verified),/未取得可核实结果/);
});
