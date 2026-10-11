import {describe,it,expect,vi,afterEach} from 'vitest';
import {ArchiveReader} from './archiveReader';
import {ApiError, type ApiClient} from '../../core/api';
import type {ArchivePage} from '../../types/archives';

const page:ArchivePage={items:[],generation:'1:1',coverage:{complete:true},index_status:'ready'};
function api(){return {getArchives:vi.fn().mockResolvedValue(page),getArchiveEntities:vi.fn().mockResolvedValue({items:[],coverage:{complete:true}}),analyzeArchives:vi.fn().mockResolvedValue({data_epoch:1,generation:1,groups:[{value:'200',currency:'CNY'}],coverage:{complete:true}}),getArchiveDocument:vi.fn()} as unknown as ApiClient;}
afterEach(()=>vi.useRealTimers());
describe('archive authorization lifetime',()=>{
 it('hides stale content at 30s even while history is expanded and unrelated requests work',async()=>{
  vi.useFakeTimers();const a=api();const reader=new ArchiveReader(a);await reader.load();expect(reader.value.snapshot).not.toBeNull();
  vi.mocked(a.getArchives).mockRejectedValue(new Error('offline'));
  await vi.advanceTimersByTimeAsync(30001);expect(reader.value.snapshot).toBeNull();reader.stop();
 });
 it('invalidates synchronously and rejects a response from the old authorization',async()=>{
  vi.useFakeTimers();const a=api();const reader=new ArchiveReader(a);await reader.load();
  let resolve!:(p:ArchivePage)=>void;vi.mocked(a.getArchives).mockReturnValue(new Promise(r=>{resolve=r;}));const old=reader.load();reader.invalidate();expect(reader.value.snapshot).toBeNull();resolve(page);await old;expect(reader.value.snapshot).toBeNull();reader.stop();
 });
 it('rebuilds every loaded page without deriving totals from that page',async()=>{
  vi.useFakeTimers();const a=api();vi.mocked(a.getArchives).mockResolvedValueOnce({...page,next_cursor:'second'}).mockResolvedValueOnce({...page,next_cursor:'second'}).mockResolvedValueOnce(page);
  const reader=new ArchiveReader(a);await reader.load();await reader.more();expect(vi.mocked(a.getArchives).mock.calls).toHaveLength(3);expect(reader.value.snapshot?.analysis?.groups[0]?.value).toBe('200');reader.stop();
 });
});

it('refuses a mixed list and analysis generation instead of showing stale totals',async()=>{
 vi.useFakeTimers();const a=api();vi.mocked(a.analyzeArchives).mockResolvedValue({data_epoch:2,generation:1,groups:[],coverage:{complete:true}} as never);
 const reader=new ArchiveReader(a);await reader.load();expect(reader.value.snapshot).toBeNull();expect(reader.value.failed).toBe(true);reader.stop();
});
it('expires the verified content from request start rather than slow response arrival',async()=>{
 vi.useFakeTimers();const a=api();const reader=new ArchiveReader(a);
 vi.mocked(a.getArchives).mockImplementation(async()=>{await new Promise(r=>setTimeout(r,11000));return page;});
 const pending=reader.load();await vi.advanceTimersByTimeAsync(11000);await pending;expect(reader.value.snapshot?.at).toBe(performance.now()-11000);
 vi.mocked(a.getArchives).mockRejectedValue(new Error('offline'));await vi.advanceTimersByTimeAsync(19000);expect(reader.value.snapshot).toBeNull();reader.stop();
});
it('revalidates expanded entity filters instead of keeping names from old pages',async()=>{
 vi.useFakeTimers();const a=api();vi.mocked(a.getArchiveEntities).mockResolvedValueOnce({items:[],next_cursor:'people-next',coverage:{complete:true}}).mockResolvedValueOnce({items:[],next_cursor:'people-next',coverage:{complete:true}}).mockResolvedValueOnce({items:[{id:'house',kind:'property',names:['合成房屋'],aliases:[]}],coverage:{complete:true}});
 const reader=new ArchiveReader(a);await reader.load();await reader.moreEntities();expect(reader.value.snapshot?.entities.items[0]?.names).toEqual(['合成房屋']);
 vi.mocked(a.getArchiveEntities).mockResolvedValue({items:[],coverage:{complete:true}});reader.invalidate();expect(reader.value.snapshot).toBeNull();await reader.load();expect(reader.value.snapshot?.entities.items).toEqual([]);reader.stop();
});

it('uses elapsed time for revalidation and expiry despite wall clock rollback',async()=>{
 vi.useFakeTimers();const a=api();const reader=new ArchiveReader(a);await reader.load();vi.mocked(a.getArchives).mockRejectedValue(new Error('offline'));vi.setSystemTime(new Date(Date.now()-3600000));await vi.advanceTimersByTimeAsync(16000);expect(vi.mocked(a.getArchives).mock.calls.length).toBeGreaterThan(1);await vi.advanceTimersByTimeAsync(15000);expect(reader.value.snapshot).toBeNull();reader.stop();
});

it('keeps freshly verified archives when the full analysis exceeds the response bound',async()=>{
 vi.useFakeTimers();const a=api();vi.mocked(a.analyzeArchives).mockRejectedValue(new ApiError(409,'conflict','bounded',null,{reason:'analysis_response_limit'}));
 const reader=new ArchiveReader(a);await reader.load();expect(reader.value.failed).toBe(false);expect(reader.value.snapshot?.analysis).toBeNull();expect(reader.value.snapshot?.page.generation).toBe('1:1');expect(a.getArchives).toHaveBeenCalledTimes(2);reader.stop();
});
it('does not downgrade authorization or generation conflicts to unavailable statistics',async()=>{
 vi.useFakeTimers();for(const error of [new ApiError(403,'forbidden','revoked'),new ApiError(409,'conflict','generation changed')]){const a=api();vi.mocked(a.analyzeArchives).mockRejectedValue(error);const reader=new ArchiveReader(a);await reader.load();expect(reader.value.snapshot).toBeNull();expect(reader.value.failed).toBe(true);reader.stop();}
});
it('rejects a generation change while revalidating a bounded analysis fallback',async()=>{
 vi.useFakeTimers();const a=api();vi.mocked(a.analyzeArchives).mockRejectedValue(new ApiError(409,'conflict','bounded',null,{reason:'analysis_response_limit'}));vi.mocked(a.getArchives).mockResolvedValueOnce(page).mockResolvedValueOnce({...page,generation:'2:1'});const reader=new ArchiveReader(a);await reader.load();expect(reader.value.snapshot).toBeNull();expect(reader.value.failed).toBe(true);reader.stop();
});

it('loads the milk chart from the whole authorized day rather than the current archive page',async()=>{
 vi.useFakeTimers();vi.setSystemTime(new Date('2026-10-11T09:00:00Z'));
 const a=api();const reader=new ArchiveReader(a);await reader.load();
 expect(a.analyzeArchives).toHaveBeenCalledWith(expect.objectContaining({operation:'series',metric:'volume',domain:'health',date_from:'2026-10-11',date_to:'2026-10-11'}),expect.anything());
 reader.stop();
});

it('loads all 30 local days with Core decimal totals and the selected person',async()=>{
 vi.useFakeTimers();vi.setSystemTime(new Date('2026-10-10T17:00:00Z'));
 const a=api();vi.mocked(a.analyzeArchives).mockImplementation(async q=>({data_epoch:1,generation:1,groups:q.date_from==='2026-10-11'?[{metric:'volume',unit:'ml',value:'120.125'}]:[],coverage:{complete:true}} as never));
 const reader=new ArchiveReader(a);reader.filter={domain:'health',entity_id:'baby',query:'喂奶'};await reader.selectPeriod('month');
 const calls=vi.mocked(a.analyzeArchives).mock.calls.map(([q])=>q);
 expect(calls).toHaveLength(30);expect(new Set(calls.map(q=>q.date_from)).size).toBe(30);
 expect(calls[0]).toEqual(expect.objectContaining({operation:'sum',metric:'volume',domain:'health',entity_id:'baby',query:'喂奶',date_from:'2026-09-12',date_to:'2026-09-12'}));
 expect(calls).toContainEqual(expect.objectContaining({date_from:'2026-10-11',date_to:'2026-10-11'}));
 expect(reader.value.snapshot?.milk?.groups).toEqual([{metric:'volume',unit:'ml',value:'120.125',date:'2026-10-11',date_precision:'day'}]);
 reader.stop();
});

it('hides a monthly chart if one day belongs to a different authorization generation',async()=>{
 vi.useFakeTimers();vi.setSystemTime(new Date('2026-10-11T09:00:00Z'));
 const a=api();vi.mocked(a.analyzeArchives).mockImplementation(async q=>({data_epoch:1,generation:q.date_from==='2026-10-10'?2:1,groups:[],coverage:{complete:true}} as never));
 const reader=new ArchiveReader(a);reader.filter={domain:'health'};await reader.selectPeriod('month');
 expect(reader.value.snapshot).toBeNull();expect(reader.value.failed).toBe(true);reader.stop();
});
