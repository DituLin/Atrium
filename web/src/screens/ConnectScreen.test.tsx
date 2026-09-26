/**
 * W-303 — `session.superseded` must be visible on the connect screen, with a
 * way back that does not need a reload (design §7.2).
 */

import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import type { ReactElement } from 'react';
import { useEffect } from 'react';
import { describe, expect, it, vi } from 'vitest';

import { CurrentScreen } from '../App';
import { AppProvider } from '../app/AppProvider';
import { useApp } from '../app/context';
import type { AppAction } from '../app/state';
import { createInitialState } from '../app/state';
import { AppContext } from '../app/context';
import { ApiClient } from '../core/api';
import { ConnectScreen } from './ConnectScreen';

class StubSocket {
  onopen: ((e: unknown) => void) | null = null;
  onclose: ((e: unknown) => void) | null = null;
  onerror: ((e: unknown) => void) | null = null;
  onmessage: ((e: unknown) => void) | null = null;
  send(): void {}
  close(): void {}
}

function stubApi(): void {
  vi.stubGlobal('WebSocket', StubSocket);
  vi.stubGlobal('fetch', vi.fn(() => Promise.reject(new Error('core unreachable'))));
}

/**
 * The provider's own connection effect runs after the children's, and
 * `connection.start` clears the stop reason, so the superseded event is
 * injected after the first paint — which is also when it arrives in reality.
 */
function Harness(props: { onReady: (dispatch: (action: AppAction) => void) => void }): ReactElement {
  const { state, dispatch } = useApp();
  useEffect(() => {
    props.onReady(dispatch);
  }, [dispatch, props]);
  return (
    <>
      <CurrentScreen />
      <output data-testid="nonce">{state.connection.retryNonce}</output>
    </>
  );
}

describe('connect screen (W-303)', () => {
  it('explains a superseded session and offers a manual retry', async () => {
    stubApi();
    let dispatch: ((action: AppAction) => void) | null = null;
    render(
      <AppProvider>
        <Harness
          onReady={(next) => {
            dispatch = next;
          }}
        />
      </AppProvider>,
    );
    await waitFor(() => expect(dispatch).not.toBeNull());
    act(() => {
      (dispatch as unknown as (action: AppAction) => void)({ type: 'connection.superseded' });
    });

    await waitFor(() => expect(screen.getByText(/此屏幕已在另一处连接/)).toBeDefined());
    expect(screen.getByText(/同一设备的新连接已接替此页面/)).toBeDefined();

    const button = screen.getByRole('button', { name: '重新连接此屏幕' });
    // The remote has no pointer, so the only actionable control holds focus.
    await waitFor(() => expect(document.activeElement).toBe(button));

    fireEvent.click(button);
    await waitFor(() => expect(screen.getByTestId('nonce').textContent).toBe('1'));
    expect(screen.queryByText(/此屏幕已在另一处连接/)).toBeNull();
  });
});

describe('connect screen states', () => {
  function renderConnect(patch: (state: ReturnType<typeof createInitialState>) => void) {
    const state = createInitialState(Date.parse('2026-09-26T13:24:00Z'));
    patch(state);
    const loader = { load: async () => {}, invalidate: () => {} };
    const api = new ApiClient({ fetchImpl: () => new Promise(() => {}) });
    render(<AppContext.Provider value={{ state, dispatch: vi.fn(), overview: loader, house: loader, api, goBack() {}, clientVersion: 'test' }}><ConnectScreen /></AppContext.Provider>);
  }

  it('reports consecutive failures and the last close code while retrying automatically', () => {
    renderConnect(state => { state.connection = { ...state.connection, status: 'reconnecting', failures: 3, lastCloseCode: 1006 }; });
    expect(screen.getByText('正在连接家庭服务')).toBeDefined();
    expect(screen.getByText('已连续失败 3 次 · 系统会自动重试')).toBeDefined();
    expect(screen.getByText('1006')).toBeDefined();
    const retry = screen.getByRole('button', { name: '立即重试' });
    expect(document.activeElement).toBe(retry);
    fireEvent.keyDown(retry, { key: 'ArrowDown' });
    expect(document.activeElement).toBe(screen.getByRole('button', { name: '查看中枢状态' }));
  });

  it('explains an expired authorization and keeps the family clock only when its zone is known', () => {
    renderConnect(state => {
      state.authExpired = true;
      state.home = { schema_version: 1, server_time: '2026-09-26T13:24:00Z', version: 1, home: { name: '家', timezone: 'Asia/Singapore' },
        widgets: [{ type: 'clock', payload: { timezone: 'Asia/Singapore', utc_offset_seconds: 28800 } }] } as never;
    });
    expect(screen.getByText('需要重新确认屏幕授权')).toBeDefined();
    expect(screen.getByText(/超过 24 小时未成功确认授权/)).toBeDefined();
    expect(screen.getByText('21:24')).toBeDefined();
  });

  it('shows no invented time before any home snapshot', () => {
    renderConnect(() => {});
    expect(screen.queryByText(/时间由电视继续走时/)).toBeNull();
  });
});
