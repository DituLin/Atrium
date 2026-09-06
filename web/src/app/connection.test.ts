import { describe, expect, it } from 'vitest';

import { closeCodeAction } from '../core/ws';
import type { ConnectionState } from './connection';
import {
  OFFLINE_AFTER_FAILURES,
  connectionReducer,
  initialConnectionState,
  needsConnectScreen,
  showsReconnectingBanner,
} from './connection';

function run(actions: Parameters<typeof connectionReducer>[1][]): ConnectionState {
  return actions.reduce(connectionReducer, initialConnectionState);
}

describe('connection state machine (design §7.2)', () => {
  it('walks idle → connecting → online', () => {
    const state = run([
      { type: 'connection.start' },
      { type: 'connection.snapshotOk' },
      { type: 'connection.socketOpen' },
    ]);
    expect(state.status).toBe('online');
    expect(state.hasSnapshot).toBe(true);
    expect(state.failures).toBe(0);
  });

  it('degrades to reconnecting and then offline', () => {
    let state = run([
      { type: 'connection.start' },
      { type: 'connection.snapshotOk' },
      { type: 'connection.socketOpen' },
      { type: 'connection.attemptFailed' },
    ]);
    expect(state.status).toBe('reconnecting');
    expect(showsReconnectingBanner(state)).toBe(true);
    expect(needsConnectScreen(state)).toBe(false);
    for (let i = 1; i < OFFLINE_AFTER_FAILURES; i += 1) {
      state = connectionReducer(state, { type: 'connection.attemptFailed' });
    }
    expect(state.status).toBe('offline');
    // Something is cached, so the dashboard keeps rendering behind a banner.
    expect(needsConnectScreen(state)).toBe(false);
  });

  it('shows the connect screen when nothing is cached', () => {
    const state = run([{ type: 'connection.start' }, { type: 'connection.attemptFailed' }]);
    expect(needsConnectScreen(state)).toBe(true);
  });

  it('4003 superseded stops and always shows the connect screen', () => {
    const state = run([
      { type: 'connection.start' },
      { type: 'connection.snapshotOk' },
      { type: 'connection.socketOpen' },
      { type: 'connection.closed', code: 4003, action: closeCodeAction(4003) },
    ]);
    expect(state.stopReason).toBe('superseded');
    expect(needsConnectScreen(state)).toBe(true);
    expect(showsReconnectingBanner(state)).toBe(false);
  });

  it('4002 revoked drops the snapshot', () => {
    const state = run([
      { type: 'connection.start' },
      { type: 'connection.snapshotOk' },
      { type: 'connection.socketOpen' },
      { type: 'connection.closed', code: 4002, action: closeCodeAction(4002) },
    ]);
    expect(state.stopReason).toBe('revoked');
    expect(state.hasSnapshot).toBe(false);
    expect(needsConnectScreen(state)).toBe(true);
  });

  it('401 marks the session unauthorized', () => {
    const state = run([{ type: 'connection.start' }, { type: 'connection.unauthorized' }]);
    expect(state.stopReason).toBe('unauthorized');
    expect(state.status).toBe('offline');
  });

  it('4004 and 4005 keep reconnecting', () => {
    for (const code of [4004, 4005, 1006, 1001]) {
      const state = run([
        { type: 'connection.start' },
        { type: 'connection.snapshotOk' },
        { type: 'connection.socketOpen' },
        { type: 'connection.closed', code, action: closeCodeAction(code) },
      ]);
      expect(state.stopReason, String(code)).toBeNull();
      expect(state.status, String(code)).toBe('reconnecting');
    }
  });
});

describe('superseded and manual retry (W-303)', () => {
  it('the session.superseded message alone puts the connect screen in charge', () => {
    const state = run([
      { type: 'connection.start' },
      { type: 'connection.snapshotOk' },
      { type: 'connection.socketOpen' },
      { type: 'connection.superseded' },
    ]);
    expect(state.stopReason).toBe('superseded');
    expect(needsConnectScreen(state)).toBe(true);
    expect(showsReconnectingBanner(state)).toBe(false);
  });

  it('a manual retry clears the stop reason and bumps the nonce the socket keys off', () => {
    const state = run([
      { type: 'connection.start' },
      { type: 'connection.snapshotOk' },
      { type: 'connection.socketOpen' },
      { type: 'connection.closed', code: 4003, action: closeCodeAction(4003) },
      { type: 'connection.retry' },
    ]);
    expect(state.stopReason).toBeNull();
    expect(state.retryNonce).toBe(1);
    expect(state.failures).toBe(0);
    expect(state.status).toBe('reconnecting');
    expect(needsConnectScreen(state)).toBe(false);
  });
});
