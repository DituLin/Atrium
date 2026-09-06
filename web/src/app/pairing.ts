/**
 * Pairing state machine (design §6.8, §7.1).
 *
 *   idle → starting → waiting ⇄ (poll) → approved → claiming → claimed
 *                   ↘ expired / error → restart
 *
 * The TV shows the code and polls `GET /pair/{id}` every `poll_interval_ms`.
 * Expiry restarts the whole flow with a fresh code; a claim moves the app to
 * the dashboard.
 */

import type { PairingStatus } from '../types/api';

export type PairPhase =
  | 'idle'
  | 'starting'
  | 'waiting'
  | 'approved'
  | 'claiming'
  | 'claimed'
  | 'expired'
  | 'error';

export interface PairingState {
  phase: PairPhase;
  pairingId: string | null;
  code: string | null;
  expiresAt: string | null;
  pollIntervalMs: number;
  screenName: string | null;
  errorMessage: string | null;
}

export const DEFAULT_POLL_INTERVAL_MS = 3000;
/** Guard against a server sending an absurd interval. */
export const MIN_POLL_INTERVAL_MS = 1000;
export const MAX_POLL_INTERVAL_MS = 30_000;

export const initialPairingState: PairingState = {
  phase: 'idle',
  pairingId: null,
  code: null,
  expiresAt: null,
  pollIntervalMs: DEFAULT_POLL_INTERVAL_MS,
  screenName: null,
  errorMessage: null,
};

export function clampPollInterval(ms: number | undefined): number {
  if (typeof ms !== 'number' || !Number.isFinite(ms)) return DEFAULT_POLL_INTERVAL_MS;
  return Math.min(MAX_POLL_INTERVAL_MS, Math.max(MIN_POLL_INTERVAL_MS, Math.round(ms)));
}

export type PairingAction =
  | { type: 'pair.start' }
  | {
      type: 'pair.started';
      pairingId: string;
      code: string;
      expiresAt: string;
      pollIntervalMs: number;
    }
  | { type: 'pair.status'; status: PairingStatus }
  | { type: 'pair.claiming' }
  | { type: 'pair.claimed'; screenName: string }
  | { type: 'pair.expired' }
  | { type: 'pair.error'; message: string }
  | { type: 'pair.reset' };

export function pairingReducer(state: PairingState, action: PairingAction): PairingState {
  switch (action.type) {
    case 'pair.start':
      return { ...initialPairingState, phase: 'starting' };
    case 'pair.started':
      return {
        phase: 'waiting',
        pairingId: action.pairingId,
        code: action.code,
        expiresAt: action.expiresAt,
        pollIntervalMs: clampPollInterval(action.pollIntervalMs),
        screenName: null,
        errorMessage: null,
      };
    case 'pair.status':
      switch (action.status) {
        case 'approved':
          return state.phase === 'waiting' ? { ...state, phase: 'approved' } : state;
        case 'claimed':
          // Claimed elsewhere (or a retry raced us): restart from a clean code.
          return state.phase === 'claimed' ? state : { ...state, phase: 'expired' };
        case 'expired':
        case 'rejected':
          return { ...state, phase: 'expired' };
        case 'pending':
        default:
          return state;
      }
    case 'pair.claiming':
      return state.phase === 'approved' ? { ...state, phase: 'claiming' } : state;
    case 'pair.claimed':
      return { ...state, phase: 'claimed', screenName: action.screenName, errorMessage: null };
    case 'pair.expired':
      return { ...state, phase: 'expired' };
    case 'pair.error':
      return { ...state, phase: 'error', errorMessage: action.message };
    case 'pair.reset':
      return initialPairingState;
    default:
      return state;
  }
}

/** True while the poller should keep running. */
export function shouldPoll(state: PairingState): boolean {
  return state.phase === 'waiting' && state.pairingId !== null;
}

/** Local expiry check used only to restart the code, never for authorization. */
export function isCodeExpired(state: PairingState, nowMs: number): boolean {
  if (!state.expiresAt) return false;
  const expiry = Date.parse(state.expiresAt);
  return Number.isNaN(expiry) ? false : nowMs >= expiry;
}
