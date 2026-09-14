import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { useEffect } from 'react';
import { describe, expect, it, vi } from 'vitest';
import { AppProvider } from '../app/AppProvider';
import { useApp } from '../app/context';
import { CurrentScreen } from '../App';
import type { AppAction, AppState } from '../app/state';
import { saveLastAuthOkAt } from '../core/authExpiry';

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
      : path.includes('/photos?') ? { items: [], next_cursor: null } : HOME), { status: 200 }))));
  render(<AppProvider><Harness /></AppProvider>);
  await screen.findByRole('button', { name: '打开当前照片' });
}
function key(key: string, repeat = false) { return fireEvent.keyDown(document.activeElement!, { key, repeat }); }

describe('settings and Back ownership', () => {
  it('confirms sidebar selection, moves right to recovery, and keeps focus during status updates', async () => {
    await setup(); key('ArrowRight'); key('Enter');
    const connection = await screen.findByRole('tab', { name: '连接状态' });
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
  it('returns settings to home status, then consumes Back to hero, then leaves root Back unhandled', async () => {
    await setup(); key('ArrowRight'); key('Enter');
    await screen.findByRole('tab', { name: '连接状态' });
    expect(key('Escape')).toBe(false);
    expect(document.activeElement).toBe(screen.getByRole('button', { name: '查看状态' }));
    expect(key('Escape', true)).toBe(false);
    expect(document.activeElement).toBe(screen.getByRole('button', { name: '查看状态' }));
    expect(key('Escape')).toBe(false);
    expect(document.activeElement).toBe(screen.getByRole('button', { name: '打开当前照片' }));
    expect(key('Escape')).toBe(true);
  });
  it('returns settings to photos navigation and gallery to the original home navigation entry', async () => {
    await setup(); key('ArrowDown'); key('ArrowRight'); key('Enter');
    await screen.findByRole('tab', { name: '最近新增' });
    key('ArrowUp'); key('ArrowRight'); key('ArrowRight'); key('ArrowRight'); key('ArrowRight'); key('Enter');
    await screen.findByRole('tab', { name: '连接状态' });
    key('Escape');
    await waitFor(() => expect(document.activeElement).toBe(screen.getByRole('button', { name: '设置' })));
    key('Escape');
    expect(document.activeElement).toBe(screen.getByRole('button', { name: '照片' }));
  });
});

it('keeps missing NAS information distinct from a confirmed empty sources list', async () => {
  await setup();
  act(() => dispatch({ type: 'app.homeLoaded', home: { ...HOME, widgets: [] } as never, receivedAt: Date.now() }));
  key('ArrowRight'); key('Enter');
  await screen.findByRole('tab', { name: '照片来源' });
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
  await screen.findByRole('button', { name: '打开当前照片' });
});

it('returns from photos to the original settings navigation entry', async () => {
  await setup(); key('ArrowRight'); key('Enter');
  await screen.findByRole('tab', { name: '连接状态' });
  fireEvent.keyDown(document.activeElement!, { key: 'ArrowDown' });
  key('ArrowDown'); key('ArrowDown'); key('ArrowLeft'); key('ArrowLeft'); key('ArrowLeft'); key('ArrowLeft'); key('Enter');
  await screen.findByRole('tab', { name: '最近新增' });
  key('Escape');
  expect(document.activeElement).toBe(screen.getByRole('button', { name: '照片' }));
});

it('returns settings to the exact empty-gallery status recovery button', async () => {
  await setup(); key('ArrowDown'); key('ArrowRight'); key('Enter');
  await screen.findByRole('button', { name: '查看全部照片' });
  key('ArrowDown'); key('ArrowRight'); key('Enter');
  await screen.findByRole('tab', { name: '连接状态' });
  key('Escape');
  await waitFor(() => expect(document.activeElement).toBe(screen.getByRole('button', { name: '查看状态' })));
});

it('lets the remote enter and scroll long status details from the persistent recovery action', async () => {
  await setup(); key('ArrowRight'); key('Enter');
  await screen.findByRole('tab', { name: '连接状态' });
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
  await setup(); key('ArrowDown'); key('ArrowRight'); key('Enter');
  await screen.findByRole('button', { name: '查看全部照片' });
  key('ArrowDown'); key('ArrowRight'); key('Enter');
  await screen.findByRole('tab', { name: '连接状态' });
  const item = { id: 'new-photo', source_id: 'photos', captured_at: null, captured_confidence: 'unknown' as const,
    first_seen_at: new Date().toISOString(), is_baseline: false, width: 1600, height: 1200,
    preview: { status: 'ready' as const, width: 1600, height: 1200 }, urls: { thumb: '/thumb/new', preview: '/preview/new' } };
  act(() => dispatch({ type: 'photos.pageLoaded', collection: 'recent', generation: currentState.collection.generation,
    items: [item], nextCursor: null, meta: null, append: false }));
  key('Escape');
  expect(screen.queryByRole('button', { name: '查看状态' })).toBeNull();
  expect(document.activeElement).toBe(screen.getByRole('tab', { name: '最近新增' }));
  key('ArrowDown');
  expect(document.activeElement?.classList.contains('thumb')).toBe(true);
});
