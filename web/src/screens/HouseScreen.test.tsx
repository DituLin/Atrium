import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { useEffect } from 'react';
import { expect, it, vi } from 'vitest';
import { AppProvider } from '../app/AppProvider';
import { useApp } from '../app/context';
import type { AppContextValue } from '../app/context';
import { CurrentScreen } from '../App';
import { houseFixture } from '../test/houseFixture';
import { overviewFixture } from '../test/overviewFixture';
import { saveLastAuthOkAt } from '../core/authExpiry';
let app: AppContextValue;
function Harness() { const value = useApp(); useEffect(() => { app = value; }, [value]); return <CurrentScreen />; }
function key(key: string) { fireEvent.keyDown(document.activeElement!, { key }); }
async function setup(homeFailed = false) {
  saveLastAuthOkAt(Date.now());
  vi.stubGlobal('WebSocket', class { send() {} close() {} });
  const fetch = vi.fn(async (path: string) => {
    if (path.includes('/family/house')) return new Response(JSON.stringify(houseFixture()));
    if (path.includes('/family/overview')) return new Response(JSON.stringify(overviewFixture()));
    if (path.includes('/home') && homeFailed) return new Response('{}', { status: 500 });
    if (path.includes('/home')) return new Response(JSON.stringify({ schema_version: 1, server_time: new Date().toISOString(), version: 1, home: houseFixture().home, widgets: [] }));
    return new Response(JSON.stringify({ items: [], next_cursor: null }));
  });
  vi.stubGlobal('fetch', fetch); render(<AppProvider><Harness /></AppProvider>);
  await waitFor(() => expect(app).toBeDefined());
  return fetch;
}
it('opens independently through a fresh authenticated House request when home fails, using only remote keys', async () => {
  const fetch = await setup(true);
  await screen.findByRole('button', { name: '立即重试' });
  key('ArrowDown'); expect(document.activeElement).toBe(screen.getByRole('button', { name: '查看中枢状态' })); key('Enter');
  await screen.findByRole('region', { name: '中枢与来源' });
  expect(fetch.mock.calls.some(([path]) => path.includes('/family/house'))).toBe(true);
  expect(screen.getByText('离线')).toBeDefined(); expect(screen.getByText('房屋资料尚未填写')).toBeDefined(); expect(screen.getByText('未接入')).toBeDefined();
  expect(screen.getByText(/实时连接未建立/)).toBeDefined();
});
async function enter() {
  act(() => app.dispatch({ type: 'router.navigate', route: { name: 'house' }, sourceFocus: 'nav-house' }));
  return screen.findByRole('region', { name: '中枢与来源' });
}
function nav(label: string) { return screen.getByRole('button', { name: label }); }
it('keeps details scrolling, recheck focus and full nav/back roundtrips under the remote', async () => {
  await setup();
  const details = await enter(); expect(document.activeElement).toBe(details);
  expect(details.querySelector('.info-house__profile')).toBeNull();
  Object.defineProperties(details, { clientHeight: { value: 100 }, scrollHeight: { value: 300 } });
  key('ArrowDown'); expect(details.scrollTop).toBe(70); key('ArrowUp'); expect(details.scrollTop).toBe(0);
  key('ArrowRight'); const recheck = screen.getByRole('button', { name: '重新检查' }); expect(document.activeElement).toBe(recheck);
  key('Enter'); await screen.findByText('已更新状态'); expect(document.activeElement).toBe(recheck);
  key('ArrowUp'); expect(document.activeElement).toBe(details); key('ArrowUp'); expect(document.activeElement).toBe(nav('房屋'));
  key('ArrowDown'); expect(document.activeElement).toBe(details); key('ArrowUp'); key('ArrowRight'); key('Enter');
  await screen.findByRole('tab', { name: '连接状态' }); key('Escape');
  await waitFor(() => expect(document.activeElement).toBe(nav('设置')));
  key('ArrowLeft'); key('ArrowLeft'); key('Enter'); await screen.findByRole('region', { name: '今日事项' }); key('Escape');
  await waitFor(() => expect(document.activeElement).toBe(nav('今日'))); expect(app.state.router.route.name).toBe('house');
});
it('purges House synchronously on revocation and will not display a late pre-pair response', async () => {
  const fetch = await setup(); await waitFor(() => expect(app.state.home).not.toBeNull());
  let finish!: (response: Response) => void; fetch.mockImplementationOnce(() => new Promise(resolve => { finish = resolve; }));
  let old!: Promise<void>; act(() => { old = app.house.load().catch(() => {}); });
  act(() => { app.dispatch({ type: 'app.needsPairing' }); finish(new Response(JSON.stringify(houseFixture()))); });
  await old; expect(app.state.house.snapshot).toBeNull(); expect(app.state.needsPairing).toBe(true);
});

it('requires a fresh successful House response after expiry and never exposes the action when superseded', async () => {
  const fetch = await setup(true);
  act(() => app.dispatch({ type: 'app.authExpired' }));
  await screen.findByRole('button', { name: '立即重试' });
  key('ArrowDown'); key('Enter');
  await screen.findByRole('region', { name: '中枢与来源' }); expect(app.state.authExpired).toBe(false);
  expect(app.state.house.snapshot?.home.name).toBe('测试家庭');
  act(() => app.dispatch({ type: 'connection.superseded' }));
  await screen.findByRole('button', { name: '重新连接此屏幕' });
  expect(screen.queryByRole('button', { name: '查看中枢状态' })).toBeNull();
  expect(fetch.mock.calls.filter(([path]) => path.includes('/family/house')).length).toBeGreaterThan(0);
});
it('HTTP-only revoked authorization purges family and blocks the House recovery route', async () => {
  const fetch = await setup(true);
  await screen.findByRole('button', { name: '立即重试' }); key('ArrowDown'); key('Enter');
  await screen.findByRole('region', { name: '中枢与来源' });
  fetch.mockImplementation(async () => new Response(JSON.stringify({ error: { code: 'screen_revoked' } }), { status: 410 }));
  key('ArrowRight'); key('Enter');
  await waitFor(() => expect(app.state.needsPairing).toBe(true));
  expect(app.state.house.snapshot).toBeNull(); expect(screen.queryByRole('region', { name: '中枢与来源' })).toBeNull();
  act(() => app.dispatch({ type: 'app.authOk' })); expect(app.state.needsPairing).toBe(true);
});
it('renders first-check, failed, empty and expired observations distinctly, while updates never steal focus', async () => {
  const fetch = await setup(); await enter();
  await screen.findByText('离线'); key('ArrowRight'); const recheck = screen.getByRole('button', { name: '重新检查' });
  const replace = async (snapshot: ReturnType<typeof houseFixture>) => {
    fetch.mockResolvedValueOnce(new Response(JSON.stringify(snapshot))); key('Enter');
    await waitFor(() => expect(app.state.house.snapshot).toEqual(snapshot)); expect(document.activeElement).toBe(recheck);
  };
  const loading = houseFixture(); loading.nas[0] = { ...loading.nas[0]!, availability: 'loading', reason: 'not_observed', items: [], observed_at: null, expires_at: null };
  await replace(loading); expect(screen.getByText('尚未完成首次检查')).toBeDefined(); expect(screen.queryByText('没有可展示的照片来源')).toBeNull();
  const failed = houseFixture(); failed.nas[0] = { ...failed.nas[0]!, availability: 'failed', reason: 'read_failed', items: [], observed_at: null, expires_at: null };
  await replace(failed); expect(screen.getByText('暂时无法读取来源状态')).toBeDefined(); expect(screen.queryByText('离线')).toBeNull();
  await replace({ ...houseFixture(), nas: [] }); expect(screen.getByText('没有可展示的照片来源')).toBeDefined();
  const stale = houseFixture(); stale.nas[0] = { ...stale.nas[0]!, expires_at: stale.generated_at };
  await replace(stale); act(() => app.dispatch({ type: 'app.tick', nowMs: Date.now() + 1000 }));
  expect(screen.getByText(/上次观测：离线/)).toBeDefined(); expect(screen.getByText('最近可读')).toBeDefined(); expect(document.activeElement).toBe(recheck);
});
