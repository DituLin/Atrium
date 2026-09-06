import { describe, expect, it } from 'vitest';

import {
  DEFAULT_POLL_INTERVAL_MS,
  MAX_POLL_INTERVAL_MS,
  MIN_POLL_INTERVAL_MS,
  clampPollInterval,
  initialPairingState,
  isCodeExpired,
  pairingReducer,
  shouldPoll,
} from './pairing';

describe('pairing state machine (design §6.8)', () => {
  it('walks start → waiting → approved → claimed', () => {
    let state = pairingReducer(initialPairingState, { type: 'pair.start' });
    expect(state.phase).toBe('starting');
    state = pairingReducer(state, {
      type: 'pair.started',
      pairingId: 'pr_1',
      code: '123456',
      expiresAt: '2026-09-05T00:05:00Z',
      pollIntervalMs: 3000,
    });
    expect(state.phase).toBe('waiting');
    expect(shouldPoll(state)).toBe(true);
    state = pairingReducer(state, { type: 'pair.status', status: 'pending' });
    expect(state.phase).toBe('waiting');
    state = pairingReducer(state, { type: 'pair.status', status: 'approved' });
    state = pairingReducer(state, { type: 'pair.claiming' });
    state = pairingReducer(state, { type: 'pair.claimed', screenName: 'Living room TV' });
    expect(state.phase).toBe('claimed');
    expect(shouldPoll(state)).toBe(false);
  });

  it('restarts on expiry, rejection, and a lost claim', () => {
    const waiting = pairingReducer(initialPairingState, {
      type: 'pair.started',
      pairingId: 'pr_1',
      code: '123456',
      expiresAt: '2026-09-05T00:05:00Z',
      pollIntervalMs: 3000,
    });
    for (const status of ['expired', 'rejected', 'claimed'] as const) {
      expect(pairingReducer(waiting, { type: 'pair.status', status }).phase).toBe('expired');
    }
  });

  it('clamps a hostile poll interval', () => {
    expect(clampPollInterval(3000)).toBe(3000);
    expect(clampPollInterval(1)).toBe(MIN_POLL_INTERVAL_MS);
    expect(clampPollInterval(10 ** 9)).toBe(MAX_POLL_INTERVAL_MS);
    expect(clampPollInterval(Number.NaN)).toBe(DEFAULT_POLL_INTERVAL_MS);
    expect(clampPollInterval(undefined)).toBe(DEFAULT_POLL_INTERVAL_MS);
  });

  it('detects local code expiry only for restart purposes', () => {
    const state = { ...initialPairingState, expiresAt: '2026-09-05T00:05:00Z' };
    expect(isCodeExpired(state, Date.parse('2026-09-05T00:04:59Z'))).toBe(false);
    expect(isCodeExpired(state, Date.parse('2026-09-05T00:05:00Z'))).toBe(true);
    expect(isCodeExpired(initialPairingState, Date.now())).toBe(false);
  });
});
