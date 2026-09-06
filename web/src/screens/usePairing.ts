/**
 * Pairing driver (W-102): starts a pairing, polls at the server's
 * `poll_interval_ms`, restarts on expiry, and claims once approved.
 * All decisions live in `app/pairing.ts`; this hook is the I/O around them.
 */

import { useCallback, useEffect, useRef } from 'react';

import { useApp } from '../app/context';
import { isCodeExpired, shouldPoll } from '../app/pairing';

export function usePairing(): { restart: () => void } {
  const { state, dispatch, api } = useApp();
  const pairing = state.pairing;
  const startedRef = useRef(false);

  const start = useCallback(() => {
    dispatch({ type: 'pair.start' });
    void api
      .pairStart(navigator.userAgent.slice(0, 120))
      .then((response) =>
        dispatch({
          type: 'pair.started',
          pairingId: response.pairing_id,
          code: response.code,
          expiresAt: response.expires_at,
          pollIntervalMs: response.poll_interval_ms,
        }),
      )
      .catch((error: unknown) =>
        dispatch({
          type: 'pair.error',
          message: error instanceof Error ? error.message : 'Could not reach Atrium Core',
        }),
      );
  }, [api, dispatch]);

  // Start once when the pair screen appears; restart after expiry or an error.
  useEffect(() => {
    if (startedRef.current) return;
    if (pairing.phase === 'idle') {
      startedRef.current = true;
      start();
    }
  }, [pairing.phase, start]);

  useEffect(() => {
    if (pairing.phase === 'expired' || pairing.phase === 'error') {
      const handle = window.setTimeout(() => {
        startedRef.current = true;
        start();
      }, 3000);
      return () => window.clearTimeout(handle);
    }
    return undefined;
  }, [pairing.phase, start]);

  // Poll while waiting.
  useEffect(() => {
    if (!shouldPoll(pairing) || !pairing.pairingId) return undefined;
    const id = pairing.pairingId;
    const handle = window.setInterval(() => {
      if (isCodeExpired(pairing, Date.now())) {
        dispatch({ type: 'pair.expired' });
        return;
      }
      void api
        .pairStatus(id)
        .then((response) => dispatch({ type: 'pair.status', status: response.status }))
        .catch(() => {
          /* transient: the next tick retries */
        });
    }, pairing.pollIntervalMs);
    return () => window.clearInterval(handle);
  }, [api, dispatch, pairing]);

  // Claim as soon as the operator approves.
  useEffect(() => {
    if (pairing.phase !== 'approved' || !pairing.pairingId) return;
    const id = pairing.pairingId;
    dispatch({ type: 'pair.claiming' });
    void api
      .pairClaim(id)
      .then((claimed) => {
        dispatch({ type: 'pair.claimed', screenName: claimed.name });
        dispatch({ type: 'router.navigate', route: { name: 'dashboard' } });
        dispatch({ type: 'connection.reset' });
      })
      .catch((error: unknown) => {
        // 410 means the pairing was already claimed — start over with a new code.
        const status =
          typeof error === 'object' && error !== null && 'status' in error
            ? (error as { status: unknown }).status
            : null;
        if (status === 410 || status === 404) dispatch({ type: 'pair.expired' });
        else
          dispatch({
            type: 'pair.error',
            message: error instanceof Error ? error.message : 'Claim failed',
          });
      });
  }, [api, dispatch, pairing.phase, pairing.pairingId]);

  const restart = useCallback(() => {
    startedRef.current = true;
    start();
  }, [start]);

  return { restart };
}
