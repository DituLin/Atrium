import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { useEffect } from 'react';
import { describe, expect, it, vi } from 'vitest';
import { AppProvider } from '../app/AppProvider';
import { useApp } from '../app/context';
import { CurrentScreen } from '../App';
import type { AppAction, AppState } from '../app/state';
import { saveLastAuthOkAt } from '../core/authExpiry';
import { overviewFixture } from '../test/overviewFixture';

const HOME = { schema_version: 1, server_time: new Date().toISOString(), version: 7,
  home: { name: '测试家庭', timezone: 'Asia/Singapore' }, widgets: [{ type: 'nas', payload: { sources: [
    { id: 'photos', name: '家庭照片', health: 'offline', last_check_at: null, last_success_at: null },
  ] } }] };
class Socket { send() {} close() {} }
let dispatch: (action: AppAction) => void;
let currentState: AppState;
function Harness() {
  const app = useApp();
  useEffect(() => { dispatch = app.dispatch; currentState = app.state; }, [app.dispatch, app.state]);
  return <CurrentScreen />;
}
async function setup() {
  saveLastAuthOkAt(Date.now());
  vi.stubGlobal('WebSocket', Socket);
  vi.stubGlobal('fetch', vi.fn((path: string) => Promise.resolve(new Response(JSON.stringify(
    path.includes('/screens/me') ? { id: 'test-screen', name: '客厅屏幕', status: 'active' }
      : path.includes('/family/overview') ? overviewFixture()
      : path.includes('/photos?') ? { items: [], next_cursor: null } : HOME), { status: 200 }))));
  render(<AppProvider><Harness /></AppProvider>);
  await waitFor(() => expect(currentState.home).not.toBeNull());
}
async function openSettings(sourceFocus = 'nav-settings') {
  act(() => dispatch({ type: 'router.navigate', route: { name: 'settings' }, sourceFocus }));
  return screen.findByRole('tab', { name: '连接状态' });
}
function nav(label: string) { return screen.getByRole('button', { name: label }); }
function key(key: string, repeat = false) { return fireEvent.keyDown(document.activeElement!, { key, repeat }); }

describe('settings and Back ownership', () => {
  it('confirms sidebar selection, moves right to recovery, and keeps focus during status updates', async () => {
    await setup();
    const connection = await openSettings();
    expect(document.activeElement).toBe(connection);
    await screen.findByText('客厅屏幕');
    key('ArrowDown');
    expect(screen.getByRole('tab', { name: '连接状态' }).getAttribute('aria-selected')).toBe('true');
    key('Enter');
    expect(screen.getByRole('tab', { name: '照片来源' }).getAttribute('aria-selected')).toBe('true');
    expect(screen.getByText('家庭照片')).toBeDefined();
    expect(screen.getByText('离线')).toBeDefined();
    key('ArrowRight');
    const retry = screen.getByRole('button', { name: '重新检查' });
    expect(document.activeElement).toBe(retry);
    act(() => dispatch({ type: 'app.homeLoaded', home: HOME as never, receivedAt: Date.now() }));
    expect(document.activeElement).toBe(retry);
    key('ArrowLeft'); key('ArrowDown'); key('Enter');
    expect(screen.getByText('客户端版本')).toBeDefined();
  });
  it('reaches the top navigation from the first category and returns with Down', async () => {
    await setup(); await openSettings();
    key('ArrowUp'); expect(document.activeElement).toBe(nav('设置'));
    key('ArrowDown'); expect(document.activeElement).toBe(screen.getByRole('tab', { name: '连接状态' }));
  });
  it('returns settings to home on Back and consumes the key', async () => {
    await setup(); await openSettings();
    expect(key('Escape')).toBe(false);
    await waitFor(() => expect(currentState.router.route.name).toBe('dashboard'));
  });
  it('returns from photos to the top navigation entry that opened them', async () => {
    await setup(); await openSettings();
    key('ArrowUp'); key('ArrowLeft'); key('ArrowLeft'); key('ArrowLeft');
    expect(document.activeElement).toBe(nav('影像')); key('Enter');
    await waitFor(() => expect(currentState.router.route.name).toBe('photos'));
    key('Escape');
    await screen.findByRole('tab', { name: '连接状态' });
    await waitFor(() => expect(document.activeElement).toBe(nav('影像')));
  });
});

it('keeps missing NAS information distinct from a confirmed empty sources list', async () => {
  await setup();
  act(() => dispatch({ type: 'app.homeLoaded', home: { ...HOME, widgets: [] } as never, receivedAt: Date.now() }));
  await openSettings();
  // A later valid home snapshot may omit a widget; omission is not configuration evidence.
  act(() => dispatch({ type: 'app.homeLoaded', home: { ...HOME, widgets: [] } as never, receivedAt: Date.now() }));
  fireEvent.click(screen.getByRole('tab', { name: '照片来源' }));
  expect(screen.getByText('照片来源尚未取得')).toBeDefined();
  act(() => dispatch({ type: 'app.homeLoaded', home: { ...HOME, widgets: [{ type: 'nas', payload: { sources: [] } }] } as never, receivedAt: Date.now() }));
  expect(screen.getByText('未配置照片来源')).toBeDefined();
});

it('can recheck an expired authorization using the connect screen retry', async () => {
  await setup();
  act(() => dispatch({ type: 'app.authExpired' }));
  const retry = await screen.findByRole('button', { name: '立即重试' });
  fireEvent.keyDown(retry, { key: 'Enter' });
  await waitFor(() => expect(currentState.authExpired).toBe(false));
  expect(screen.queryByRole('button', { name: '立即重试' })).toBeNull();
});

it('returns settings to the exact gallery status recovery button', async () => {
  await setup();
  act(() => dispatch({ type: 'router.navigate', route: { name: 'photos', collection: 'recent' } }));
  const status = await screen.findByRole('button', { name: '查看状态' });
  status.focus(); key('Enter');
  await screen.findByRole('tab', { name: '连接状态' });
  key('Escape');
  await waitFor(() => expect(document.activeElement).toBe(screen.getByRole('button', { name: '查看状态' })));
});

it('lets the remote enter and scroll long status details from the persistent recovery action', async () => {
  await setup(); await openSettings();
  key('ArrowRight'); key('ArrowUp');
  const details = screen.getByRole('region', { name: '状态详情' });
  expect(document.activeElement).toBe(details);
  Object.defineProperties(details, { clientHeight: { value: 100 }, scrollHeight: { value: 400 } });
  key('ArrowDown');
  expect(details.scrollTop).toBeGreaterThan(0);
  key('ArrowLeft');
  expect(document.activeElement).toBe(screen.getByRole('tab', { name: '连接状态' }));
});

it('falls back to the selected collection when its old status entry disappears during settings', async () => {
  await setup();
  act(() => dispatch({ type: 'router.navigate', route: { name: 'photos', collection: 'recent' } }));
  const status = await screen.findByRole('button', { name: '查看状态' });
  status.focus(); key('Enter');
  await screen.findByRole('tab', { name: '连接状态' });
  const item = { id: 'new-photo', source_id: 'photos', captured_at: null, captured_confidence: 'unknown' as const,
    first_seen_at: new Date().toISOString(), is_baseline: false, width: 1600, height: 1200,
    preview: { status: 'ready' as const, width: 1600, height: 1200 }, urls: { thumb: '/thumb/new', preview: '/preview/new' } };
  act(() => dispatch({ type: 'photos.pageLoaded', collection: 'recent', generation: currentState.collection.generation,
    items: [item], nextCursor: null, meta: null, append: false }));
  key('Escape');
  await waitFor(() => expect(currentState.router.route.name).toBe('photos'));
  expect(screen.queryByRole('button', { name: '查看状态' })).toBeNull();
  // The status entry is gone, so focus must land on something inside the gallery rather than nowhere.
  expect(document.activeElement).not.toBe(document.body);
});
