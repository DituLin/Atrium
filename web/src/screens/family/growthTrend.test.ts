import { expect, it, vi } from 'vitest';
import { ApiError, type ApiClient } from '../../core/api';
import { growthRange, readGrowthTrend } from './growthTrend';
import { chartTime } from './TrendPlot';
it('uses home dates and clamps calendar month ends and leap years',()=>{
 expect(growthRange('quarter','Asia/Singapore',new Date('2026-05-30T17:00:00Z'))).toEqual({date_from:'2026-02-28',date_to:'2026-05-31'});
 expect(growthRange('year','UTC',new Date('2024-02-29T10:00:00Z'))).toEqual({date_from:'2023-02-28',date_to:'2024-02-29'});
 expect(growthRange('all','UTC')).toEqual({});
});
it('does not guess complete dates from partial or invalid observations',()=>{
 for(const date of ['2026','2026-10','2026-02-30','not a date']) expect(Number.isNaN(chartTime(date))).toBe(true);
 expect(chartTime('2026-10-13')).toBe(Date.parse('2026-10-13T00:00:00Z'));
});
it('preserves decimal measurements and reports bounded aliases without hiding the other metric',async()=>{
 const api={analyzeArchives:vi.fn(async q=>{
  if(q.metric==='身长')throw new ApiError(409,'conflict','limit',null,{reason:'analysis_response_limit'});
  return {data_epoch:1,generation:1,groups:[{metric:q.metric,value:'6.2500',unit:'kg',date:'2026-10-13'}],coverage:{complete:true}};
 })} as unknown as ApiClient;
 const result=await readGrowthTrend(api,{entity_id:'child'},'all','UTC',new AbortController().signal);
 expect(result.limited).toEqual(['height']);expect(result.weight[0]?.groups[0]?.value).toBe('6.2500');
 expect(vi.mocked(api.analyzeArchives).mock.calls.every(([q])=>q.entity_id==='child'&&q.date_from===undefined)).toBe(true);
 vi.mocked(api.analyzeArchives).mockRejectedValue(new ApiError(403,'forbidden','revoked'));
 await expect(readGrowthTrend(api,{entity_id:'child'},'all','UTC',new AbortController().signal)).rejects.toMatchObject({status:403});
});
