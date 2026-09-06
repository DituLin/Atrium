/**
 * Connection state machine (design §7.2):
 *
 *   idle → connecting → online → reconnecting → online | offline
 *
 * Each attempt first calls `GET /api/v1/home` (auth + snapshot), then opens the
 * socket. Policy for terminal conditions:
 *   401            → pair
 *   4002 revoked   → clear credential + pair
 *   4003 superseded→ stop reconnecting, show "another session is active"
 *   4001           → pair
 *   4004 / 4005    → reconnect
 */

import type { CloseAction } from '../core/ws';

export type ConnectionStatus = 'idle' | 'connecting' | 'online' | 'reconnecting' | 'offline';

/** Why the client stopped trying, if it did. */
export type ConnectionStopReason = 'superseded' | 'unauthorized' | 'revoked' | null;

export interface ConnectionState {
  status: ConnectionStatus;
  /** True once a `/home` snapshot has been received in this session. */
  hasSnapshot: boolean;
  /** Consecutive failed attempts since the last success. */
  failures: number;
  stopReason: ConnectionStopReason;
  /** Last close code seen, for diagnostics. */
  lastCloseCode: number | null;
  /**
   * Bumped by a manual retry. The socket effect keys off it, so asking again
   * after `4003 superseded` (which stops reconnecting on purpose) rebuilds the
   * client instead of waiting for a reload.
   */
  retryNonce: number;
}

export const initialConnectionState: ConnectionState = {
  status: 'idle',
  hasSnapshot: false,
  failures: 0,
  stopReason: null,
  lastCloseCode: null,
  retryNonce: 0,
};

/**
 * After this many consecutive failures the UI stops saying "reconnecting" and
 * admits it is offline. At 1 s → 30 s backoff that is roughly a minute.
 */
export const OFFLINE_AFTER_FAILURES = 3;

export type ConnectionAction =
  | { type: 'connection.start' }
  | { type: 'connection.snapshotOk' }
  | { type: 'connection.socketOpen' }
  | { type: 'connection.attemptFailed' }
  | { type: 'connection.unauthorized' }
  | { type: 'connection.closed'; code: number; action: CloseAction }
  /** `session.superseded` arrived on the socket, ahead of the 4003 close. */
  | { type: 'connection.superseded' }
  | { type: 'connection.retry' }
  | { type: 'connection.stopped' }
  | { type: 'connection.reset' };

function degrade(state: ConnectionState): ConnectionState {
  const failures = state.failures + 1;
  return {
    ...state,
    failures,
    status: failures >= OFFLINE_AFTER_FAILURES ? 'offline' : 'reconnecting',
  };
}

export function connectionReducer(
  state: ConnectionState,
  action: ConnectionAction,
): ConnectionState {
  switch (action.type) {
    case 'connection.start':
      return {
        ...state,
        status: state.status === 'online' ? 'online' : state.hasSnapshot ? 'reconnecting' : 'connecting',
        stopReason: null,
      };
    case 'connection.snapshotOk':
      return { ...state, hasSnapshot: true, stopReason: null };
    case 'connection.socketOpen':
      return { ...state, status: 'online', failures: 0, stopReason: null, lastCloseCode: null };
    case 'connection.attemptFailed':
      return degrade(state);
    case 'connection.unauthorized':
      return { ...state, status: 'offline', stopReason: 'unauthorized' };
    case 'connection.superseded':
      return { ...state, status: 'offline', stopReason: 'superseded' };
    case 'connection.retry':
      return {
        ...state,
        status: state.hasSnapshot ? 'reconnecting' : 'connecting',
        failures: 0,
        stopReason: null,
        retryNonce: state.retryNonce + 1,
      };
    case 'connection.closed': {
      const withCode = { ...state, lastCloseCode: action.code };
      switch (action.action) {
        case 'stop':
          return { ...withCode, status: 'offline', stopReason: 'superseded' };
        case 'clear-and-pair':
          return { ...withCode, status: 'offline', stopReason: 'revoked', hasSnapshot: false };
        case 'pair':
          return { ...withCode, status: 'offline', stopReason: 'unauthorized' };
        case 'idle':
          return { ...withCode, status: 'idle' };
        case 'reconnect':
        default:
          return degrade(withCode);
      }
    }
    case 'connection.stopped':
      return { ...state, status: 'idle' };
    case 'connection.reset':
      return { ...initialConnectionState, retryNonce: state.retryNonce };
    default:
      return state;
  }
}

/** The dashboard shows a banner instead of a full screen while this is true. */
export function showsReconnectingBanner(state: ConnectionState): boolean {
  return (
    state.hasSnapshot &&
    state.stopReason === null &&
    (state.status === 'reconnecting' || state.status === 'offline' || state.status === 'connecting')
  );
}

/**
 * Full connect screen: nothing cached to render, or the session was terminated
 * by the server (superseded / revoked / unauthorized).
 */
export function needsConnectScreen(state: ConnectionState): boolean {
  if (state.stopReason === 'superseded') return true;
  if (state.status === 'online') return false;
  return !state.hasSnapshot;
}
