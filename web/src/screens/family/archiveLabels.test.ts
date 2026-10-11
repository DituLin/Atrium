import {expect,it} from 'vitest';
import {archiveDate,evidenceLocation} from './archiveLabels';
it('preserves date precision and converts only complete instants to the household timezone',()=>{
 const clock={timezone:'Asia/Singapore',utcOffsetSeconds:28800};
 expect(archiveDate('2026',clock)).toBe('2026年');expect(archiveDate('2026-10',clock)).toBe('2026年10月');expect(archiveDate('2026-10-01',clock)).toBe('2026年10月1日');
 expect(archiveDate('2026-10-01T17:45:00Z',clock)).toBe('2026年10月2日 01:45');expect(archiveDate('unknown',clock)).toBe('日期未明确');
});
it('uses actual CSV record and Office locator fields',()=>{
 expect(evidenceLocation({locator:{csv:{record:7,column:3,physical_row:9,byte_column:1}}} as never)).toContain('第 7 行，第 3 列');
 expect(evidenceLocation({locator:{xlsx:{sheet_name:'家庭支出',sheet_id:'1',cell:'C9',part_uri:'/xl/worksheets/sheet1.xml'}}} as never)).toBe('家庭支出 · C9');
});
