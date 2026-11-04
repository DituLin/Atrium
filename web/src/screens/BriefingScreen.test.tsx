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
function home() {
  return { schema_version: 1, server_time: new Date().toISOString(), version: 1, home: houseFixture().home,
    widgets: [{ type: 'weather', payload: { provider: 'open-meteo', location_label: '示例城区', temperature_c: 18,
      condition_code: 2, condition_text: '多云', fetched_at: new Date().toISOString(), stale: false } }] };
}
async function setup(homeFailed = false) {
  saveLastAuthOkAt(Date.now()); vi.stubGlobal('WebSocket', class { send() {} close() {} });
  const fetch = vi.fn(async (path: string) => {
    if (path.includes('/family/overview')) return new Response(JSON.stringify(overviewFixture()));
    if (path.includes('/family/house')) return new Response(JSON.stringify(houseFixture()));
    if (path.includes('/home') && homeFailed) return new Response('{}', { status: 500 });
    if (path.includes('/home')) return new Response(JSON.stringify(home()));
    return new Response(JSON.stringify({ items: [], next_cursor: null }));
  });
  vi.stubGlobal('fetch', fetch); render(<AppProvider><Harness /></AppProvider>);
  await waitFor(() => expect(app).toBeDefined()); return fetch;
}
async function enter() {
  act(() => app.dispatch({ type: 'router.navigate', route: { name: 'briefing' }, sourceFocus: 'nav-briefing' }));
  return screen.findByRole('button', { name: '万年历' });
}

it('opens the two design cards and moves between them and the navigation with the remote', async () => {
  await setup(); const calendar = await enter(); const weather = await screen.findByRole('button', { name: '今日天气' });
  expect(document.activeElement).toBe(calendar);
  expect(screen.getByText('万年历 · 天气')).toBeDefined();
  expect(screen.queryByRole('region', { name: '今日事项' })).toBeNull();
  expect(screen.queryByRole('button', { name: '查看房屋' })).toBeNull();
  key('ArrowRight'); expect(document.activeElement).toBe(weather);
  key('ArrowUp'); expect(document.activeElement).toBe(nav('今日'));
  key('ArrowDown'); expect(document.activeElement).toBe(weather);
  key('ArrowLeft'); expect(document.activeElement).toBe(calendar);
  key('ArrowDown'); expect(document.activeElement).toBe(calendar);
  key('ArrowUp'); expect(document.activeElement).toBe(nav('今日'));
});

it('refreshes the overview and weather on OK while retaining the card focus', async () => {
  const fetch = await setup(); await enter(); await screen.findByText('18°'); key('ArrowRight');
  const weather = screen.getByRole('button', { name: '今日天气' });
  const next = home(); next.widgets[0]!.payload.temperature_c = 21;
  let resolveOverview!: (value: Response) => void;
  const pending = new Promise<Response>(resolve => { resolveOverview = resolve; });
  fetch.mockImplementation(async (path: string) => path.includes('/family/overview') ? pending
    : path.includes('/home') ? new Response(JSON.stringify(next)) : new Response('{}'));
  key('Enter');
  await screen.findByText('正在刷新…');
  await screen.findByText('21°'); expect(document.activeElement).toBe(weather);
  await act(async () => { resolveOverview(new Response(JSON.stringify(overviewFixture()))); });
  await screen.findByText('已更新简报'); expect(document.activeElement).toBe(weather);
});

it('restores the navigation entry when returning from a page opened through it', async () => {
  await setup(); await enter(); key('ArrowUp'); key('ArrowRight'); key('ArrowRight'); key('ArrowRight'); key('Enter');
  await screen.findByRole('tab', { name: '连接状态' }); key('Escape');
  await waitFor(() => expect(document.activeElement).toBe(nav('设置')));
  expect(app.state.router.route.name).toBe('briefing');
});

it('keeps the calendar available when the photo home snapshot cannot be read', async () => {
  await setup(true); await enter();
  expect(app.state.router.route.name).toBe('briefing');
  expect(screen.getByRole('button', { name: '万年历' }).textContent).not.toContain('等待家庭时间');
  expect(screen.getByRole('button', { name: '今日天气' }).textContent).toContain('暂无天气数据');
  expect(screen.queryByText('家里一切正常')).toBeNull();
});

it('purges the screen on HTTP revoke and does not render a pending pre-revocation home response', async () => {
  const fetch = await setup(); await enter(); await screen.findByText('18°'); key('ArrowRight');
  let resolveHome!: (value: Response) => void;
  const pending = new Promise<Response>(resolve => { resolveHome = resolve; });
  fetch.mockImplementation(async (path: string) => path.includes('/home') ? pending
    : new Response(JSON.stringify({ error: { code: 'screen_revoked', message: 'revoked' } }), { status: 410 }));
  key('Enter'); await waitFor(() => expect(app.state.needsPairing).toBe(true));
  await act(async () => { resolveHome(new Response(JSON.stringify(home()))); });
  expect(app.state.home).toBeNull(); expect(app.state.overview.snapshot).toBeNull();
  expect(screen.queryByRole('button', { name: '今日天气' })).toBeNull();
});
