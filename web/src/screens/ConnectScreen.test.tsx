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

    await waitFor(() => expect(screen.getByText(/Another session is active/)).toBeDefined());
    expect(screen.getByText(/replaced by a newer connection/)).toBeDefined();

    const button = screen.getByRole('button', { name: 'Take over this screen' });
    // The remote has no pointer, so the only actionable control holds focus.
    await waitFor(() => expect(document.activeElement).toBe(button));

    fireEvent.click(button);
    await waitFor(() => expect(screen.getByTestId('nonce').textContent).toBe('1'));
    expect(screen.queryByText(/Another session is active/)).toBeNull();
  });
});
