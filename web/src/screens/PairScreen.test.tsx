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
