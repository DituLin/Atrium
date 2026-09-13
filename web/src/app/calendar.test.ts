import { expect, it } from 'vitest';
import { monthCells, shiftMonth, calendarDate } from './calendar';
it('uses Monday columns and Gregorian leap/century rules in a fixed six-week sheet', () => {
 for (const [year, count] of [[2024,29],[2025,28],[1900,28],[2000,29]]) {
  const cells = monthCells(year!,2); expect(cells).toHaveLength(42); expect(cells.filter(Boolean)).toHaveLength(count!);
 }
 expect(monthCells(2026,9).slice(0,3)).toEqual([null,1,2]);
 expect(monthCells(2026,3)[6]).toBe(1);
 expect(shiftMonth({year:2026,month:12},1)).toEqual({year:2027,month:1});
 expect(shiftMonth({year:2026,month:1},-1)).toEqual({year:2025,month:12});
});
it('uses the household date across midnight and refuses an unknown zone', () => {
 expect(calendarDate(Date.parse('2026-09-12T16:00:00Z'),'Asia/Singapore')).toMatchObject({year:2026,month:9,day:13});
 expect(calendarDate(Date.parse('2026-09-12T16:00:00Z'),'America/Los_Angeles')).toMatchObject({day:12});
 expect(calendarDate(Date.now(),undefined)).toBeNull();
 expect(calendarDate(Date.now(),'bad/zone')).toBeNull();
});
