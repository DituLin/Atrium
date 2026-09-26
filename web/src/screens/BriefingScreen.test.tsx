import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { useEffect } from 'react';
import { expect, it, vi } from 'vitest';
import { AppProvider } from '../app/AppProvider';
import { useApp } from '../app/context';
import type { AppContextValue } from '../app/context';
import { CurrentScreen } from '../App';
import { overviewFixture } from '../test/overviewFixture';
import { houseFixture } from '../test/houseFixture';
import { saveLastAuthOkAt } from '../core/authExpiry';
let app: AppContextValue;
function Harness() { const value = useApp(); useEffect(() => { app = value; }, [value]); return <CurrentScreen />; }
function key(key: string) { fireEvent.keyDown(document.activeElement!, { key }); }
function nav(label: string) { return screen.getByRole('button', { name: label }); }
async function setup(homeFailed = false) {
 saveLastAuthOkAt(Date.now()); vi.stubGlobal('WebSocket', class { send() {} close() {} });
 const fetch = vi.fn(async (path: string) => {
  if (path.includes('/family/overview')) return new Response(JSON.stringify(overviewFixture()));
  if (path.includes('/family/house')) return new Response(JSON.stringify(houseFixture()));
  if (path.includes('/home') && homeFailed) return new Response('{}', { status: 500 });
  if (path.includes('/home')) return new Response(JSON.stringify({ schema_version: 1, server_time: new Date().toISOString(), version: 1, home: houseFixture().home, widgets: [] }));
  return new Response(JSON.stringify({ items: [], next_cursor: null }));
 });
 vi.stubGlobal('fetch', fetch); render(<AppProvider><Harness /></AppProvider>);
 await waitFor(() => expect(app).toBeDefined()); return fetch;
}
async function enter() {
 act(() => app.dispatch({ type: 'router.navigate', route: { name: 'briefing' }, sourceFocus: 'nav-briefing' }));
 return screen.findByRole('region', { name: '今日事项' });
}
it('reads long content with remote scrolling and restores house/action/navigation focus', async () => {
 await setup(); const reading = await enter(); await screen.findByText('测试提示正文'); expect(document.activeElement).toBe(reading);
 Object.defineProperties(reading, { clientHeight: { value: 100 }, scrollHeight: { value: 300 } });
 key('ArrowDown'); expect(reading.scrollTop).toBe(70); key('ArrowUp'); expect(reading.scrollTop).toBe(0);
 key('ArrowUp'); expect(document.activeElement).toBe(nav('今日')); key('ArrowDown'); expect(document.activeElement).toBe(reading);
 key('ArrowLeft'); expect(document.activeElement).toBe(screen.getByRole('region', { name: '万年历' })); key('ArrowRight');
 key('ArrowRight'); key('Enter'); await screen.findByRole('region', { name: '中枢与来源' }); key('Escape');
 expect(document.activeElement).toBe(screen.getByRole('button', { name: '查看房屋' }));
 key('ArrowDown'); const refresh = screen.getByRole('button', { name: '刷新简报' }); expect(document.activeElement).toBe(refresh);
 key('Enter'); await screen.findByText('已更新简报'); expect(document.activeElement).toBe(refresh);
 key('ArrowUp'); expect(document.activeElement).toBe(nav('今日'));
 key('ArrowRight'); key('ArrowRight'); key('Enter'); await screen.findByRole('tab', { name: '连接状态' }); key('Escape');
 await waitFor(() => expect(document.activeElement).toBe(nav('设置')));
 expect(app.state.router.route.name).toBe('briefing');
});
it('reaches today through authenticated House when photo home is unavailable', async () => {
 await setup(true); await screen.findByRole('button', { name: '立即重试' }); key('ArrowDown'); key('Enter');
 await screen.findByRole('region', { name: '中枢与来源' }); key('ArrowUp'); expect(document.activeElement).toBe(nav('房屋'));
 key('ArrowLeft'); key('Enter');
 await screen.findByText('测试提示正文'); expect(app.state.router.route.name).toBe('briefing');
 expect(app.state.overview.status).toBe('available'); expect(screen.queryByText('旧数据')).toBeNull();
 expect(screen.queryByText('今天没有安排')).toBeNull(); expect(screen.queryByText('家里一切正常')).toBeNull();
});
it('shows unconnected sources as not connected, never as values', async () => {
 await setup(); await enter(); await screen.findByText('测试提示正文');
 const home = screen.getByRole('complementary', { name: '简报来源' });
 expect(home.textContent).toContain('日程同步未接入'); expect(home.textContent).toContain('环境数据未接入');
 expect(home.textContent).toContain('房屋资料尚未填写'); expect(home.textContent).toContain('NAS · 家庭照片离线');
});
it('purges overview on HTTP-only revoke and discards a pending pre-revocation response', async () => {
 const fetch = await setup(); await enter(); await screen.findByText('测试提示正文');
 fetch.mockResolvedValueOnce(new Response(JSON.stringify({ error: { code: 'screen_revoked', message: 'revoked' } }), { status: 410 }));
 await act(async () => { await app.overview.load().catch(() => {}); });
 expect(app.state.overview.snapshot).toBeNull(); expect(app.state.needsPairing).toBe(true); expect(screen.queryByText('测试提示正文')).toBeNull();
});
it('replaces failed notice content, then empty content without losing refresh focus', async () => {
 const fetch = await setup(); await enter(); await screen.findByText('测试提示正文'); key('ArrowRight'); key('ArrowRight');
 const refresh = screen.getByRole('button', { name: '刷新简报' }); expect(document.activeElement).toBe(refresh);
 const failed = overviewFixture(); failed.sources.notice.availability = 'failed'; failed.sources.nas = [];
 fetch.mockResolvedValueOnce(new Response(JSON.stringify(failed))); key('Enter');
 await screen.findByText('当前没有可展示的提示'); expect(screen.getByText('暂时无法读取家庭提示。')).toBeDefined();
 expect(screen.queryByText('测试提示正文')).toBeNull(); expect(document.activeElement).toBe(refresh);
 const empty = overviewFixture(); empty.sources.nas = []; empty.sources.notice.items = [];
 fetch.mockResolvedValueOnce(new Response(JSON.stringify(empty))); key('Enter');
 await waitFor(() => expect(app.state.overview.snapshot).toEqual(empty)); expect(document.activeElement).toBe(refresh);
});
