import { fireEvent, render, screen } from '@testing-library/react';
import { expect, it, vi } from 'vitest';
import { AppContext } from '../app/context';
import { createInitialState } from '../app/state';
import { ApiClient } from '../core/api';
import { PairScreen } from './PairScreen';

it('offers a focused Chinese retry when pairing fails and consumes repeat confirmation', () => {
  const state = createInitialState(0);
  state.pairing = { ...state.pairing, phase: 'error', errorMessage: 'Network error' };
  const dispatch = vi.fn();
  const api = new ApiClient({ fetchImpl: () => new Promise(() => {}) });
  render(<AppContext.Provider value={{ overview: { load: async () => {}, invalidate: () => {} }, house: { load: async () => {}, invalidate: () => {} }, state, dispatch, api, goBack() {}, clientVersion: 'test' }}><PairScreen /></AppContext.Provider>);
  const retry = screen.getByRole('button', { name: '重新获取配对码' });
  expect(document.activeElement).toBe(retry);
  fireEvent.keyDown(retry, { key: 'Enter', repeat: true });
  expect(dispatch).not.toHaveBeenCalled();
  fireEvent.keyDown(retry, { key: 'Enter' });
  expect(dispatch).toHaveBeenCalledExactlyOnceWith({ type: 'pair.start' });
});

function renderPair(pairing: Partial<ReturnType<typeof createInitialState>['pairing']>, nowMs: number) {
  const state = createInitialState(nowMs);
  state.pairing = { ...state.pairing, ...pairing };
  const api = new ApiClient({ fetchImpl: () => new Promise(() => {}) });
  const loader = { load: async () => {}, invalidate: () => {} };
  const value = { overview: loader, house: loader, state, dispatch: vi.fn(), api, goBack() {}, clientVersion: 'test' };
  return { value, element: <AppContext.Provider value={value}><PairScreen /></AppContext.Provider> };
}

it('shows the split code, the exact approve command and a draining expiry countdown', () => {
  const issued = Date.parse('2026-09-26T13:00:00Z');
  const pairing = { phase: 'waiting' as const, pairingId: 'p1', code: '482917', expiresAt: new Date(issued + 300_000).toISOString() };
  const first = renderPair(pairing, issued);
  const { rerender } = render(first.element);
  expect(screen.getByLabelText('配对码 4 8 2 9 1 7')).toBeDefined();
  expect(screen.getByText(/atrium admin pair approve 482917/)).toBeDefined();
  expect(screen.getByLabelText('配对码剩余时间').textContent).toBe('5:00');
  const later = renderPair(pairing, issued + 84_000);
  rerender(later.element);
  expect(screen.getByLabelText('配对码剩余时间').textContent).toBe('3:36');
  expect((document.querySelector('.info-pair__bar') as HTMLElement).style.width).toBe('72%');
});

it('explains expiry without showing the stale code', () => {
  render(renderPair({ phase: 'expired', code: '482917' }, 0).element);
  expect(screen.getByText('配对码已过期，正在获取新码…')).toBeDefined();
  expect(screen.queryByLabelText(/^配对码 /)).toBeNull();
});

it('never offers a stale code in the approve command', () => {
  render(renderPair({ phase: 'expired', code: '482917' }, 0).element);
  expect(screen.queryByText(/482917/)).toBeNull();
});
