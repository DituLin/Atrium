import { act, fireEvent, render, screen } from '@testing-library/react';
import { expect, it, vi } from 'vitest';
import { AppContext } from '../app/context';
import type { AppContextValue } from '../app/context';
import { createInitialState } from '../app/state';
import { CalendarScreen } from './CalendarScreen';
import { houseFixture } from '../test/houseFixture';
function setup() {
 const now=Date.parse('2026-12-31T15:59:59Z'); vi.spyOn(Date,'now').mockReturnValue(now);
 const state=createInitialState(now); state.nowMs=now; state.clock={offsetMs:0,lastSyncAt:now};
 state.router.route={name:'calendar'}; state.house.snapshot=houseFixture(); state.house.snapshot.home.timezone='Asia/Singapore';
 const context={state,dispatch:vi.fn()} as unknown as AppContextValue;
 const ui=render(<AppContext.Provider value={context}><CalendarScreen /></AppContext.Provider>);
 return {context,ui};
}
const key=(key:string)=>fireEvent.keyDown(document.activeElement!,{key});
it('browses months with only remote keys and returns to this month without losing focus',()=>{
 const {context}=setup();
 expect(screen.getByRole('table').getAttribute('aria-label')).toBe('2026年12月');
 expect(document.activeElement).toBe(screen.getByRole('button',{name:'回到本月'}));
 key('ArrowRight'); key('Enter'); expect(screen.getByRole('table').getAttribute('aria-label')).toBe('2027年1月');
 key('ArrowLeft'); key('Enter'); expect(screen.getByRole('table').getAttribute('aria-label')).toBe('2026年12月');
 expect(document.activeElement).toBe(screen.getByRole('button',{name:'回到本月'}));
 key('ArrowDown'); expect(document.activeElement).toBe(screen.getByRole('button',{name:'日历'}));
 key('ArrowUp'); expect(document.activeElement).toBe(screen.getByRole('button',{name:'回到本月'}));
 expect(context.dispatch).not.toHaveBeenCalled();
});
it('follows household midnight on foreground but preserves an intentionally browsed month',()=>{
 const {context,ui}=setup();
 vi.mocked(Date.now).mockReturnValue(Date.parse('2026-12-31T16:00:01Z'));
 act(()=>document.dispatchEvent(new Event('visibilitychange')));
 expect(screen.getByRole('table').getAttribute('aria-label')).toBe('2027年1月');
 expect(document.querySelector('[aria-current=date]')?.textContent).toContain('1');
 key('ArrowLeft'); key('Enter');
 context.state={...context.state,nowMs:Date.parse('2027-01-31T16:00:01Z')};
 ui.rerender(<AppContext.Provider value={context}><CalendarScreen /></AppContext.Provider>);
 expect(screen.getByRole('table').getAttribute('aria-label')).toBe('2026年12月');
 expect(document.querySelector('[aria-current=date]')).toBeNull();
});
it('does not invent a household date when timezone is unavailable',()=>{
 const {context,ui}=setup(); context.state={...context.state,house:{...context.state.house,snapshot:null}};
 ui.rerender(<AppContext.Provider value={context}><CalendarScreen /></AppContext.Provider>);
 expect(screen.queryByRole('table')).toBeNull(); expect(screen.getByText('正在取得家庭时区…')).toBeTruthy();
});
it('uses a newly refreshed House timezone instead of an older Home snapshot',()=>{
 const {context,ui}=setup();
 context.state={...context.state,home:{schema_version:1,server_time:'2026-12-31T15:00:00Z',version:1,home:{name:'Home',timezone:'America/Los_Angeles'},widgets:[]}};
 const snapshot=houseFixture(); snapshot.generated_at='2026-12-31T15:59:59Z'; snapshot.home.timezone='Asia/Singapore';
 context.state.house={...context.state.house,snapshot};
 vi.mocked(Date.now).mockReturnValue(Date.parse('2026-12-31T16:00:01Z'));
 ui.rerender(<AppContext.Provider value={context}><CalendarScreen /></AppContext.Provider>);
 act(()=>document.dispatchEvent(new Event('visibilitychange')));
 expect(screen.getByRole('table').getAttribute('aria-label')).toBe('2027年1月');
 expect(screen.getByText('家庭时区 · Asia/Singapore')).toBeTruthy();
});
