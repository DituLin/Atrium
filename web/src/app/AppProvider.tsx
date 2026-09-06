/**
 * Composition root: builds the API client, runs the reducer, and wires the
 * three cross-cutting rules — the 24 h auth cache (PRD 8.2), the one-second
 * clock tick, and the remote Back key.
 */

import type { ReactElement, ReactNode } from 'react';
import { useCallback, useEffect, useMemo, useReducer, useRef } from 'react';

import { authTransport } from '../core/auth';
import { clearLastAuthOkAt, createAuthExpiryWatcher } from '../core/authExpiry';
import { createScreenApi } from './apiClient';
import type { AppContextValue } from './context';
import { AppContext } from './context';
import { toRouteState } from './router';
import { appReducer, createInitialState } from './state';
import { useConnection } from './useConnection';
import { useRemoteKeys, useSecondTick } from './useTick';

export const CLIENT_VERSION: string =
  typeof __CLIENT_VERSION__ === 'string' ? __CLIENT_VERSION__ : 'dev';

export function AppProvider(props: { children: ReactNode }): ReactElement {
  const [state, dispatch] = useReducer(appReducer, 0, createInitialState);
  const stateRef = useRef(state);
  useEffect(() => {
    stateRef.current = state;
  }, [state]);

  const api = useMemo(() => createScreenApi(dispatch), []);

  // Seed the clock immediately; the one-second tick keeps it moving.
  useEffect(() => {
    dispatch({ type: 'app.tick', nowMs: Date.now() });
  }, []);

  // PRD 8.2 — boot, visibilitychange and a 60 s timer all funnel through here.
  useEffect(() => {
    const watcher = createAuthExpiryWatcher({
      onExpired: () => {
        if (!stateRef.current.needsPairing) dispatch({ type: 'app.authExpired' });
      },
    });
    watcher.start();
    return () => watcher.stop();
  }, []);

  useSecondTick(useCallback((nowMs: number) => dispatch({ type: 'app.tick', nowMs }), []));

  const goBack = useCallback(() => dispatch({ type: 'router.back' }), []);

  useRemoteKeys(
    useCallback(
      (key) => {
        if (key === 'back') goBack();
      },
      [goBack],
    ),
  );

  // The socket runs only when the screen believes it holds a credential and the
  // auth cache has not expired. Pairing screens never open one.
  const connectionEnabled =
    !state.needsPairing && !state.authExpired && authTransport.hasCredential();
  const ws = useConnection({ api, clientVersion: CLIENT_VERSION, state, dispatch, enabled: connectionEnabled });

  // Report route changes immediately (design §6.5).
  const lastRoute = useRef(state.router.route);
  useEffect(() => {
    if (lastRoute.current === state.router.route) return;
    lastRoute.current = state.router.route;
    ws.sendState(toRouteState(state.router.route));
  }, [state.router.route, ws]);

  // A revoked screen must forget its credential and its cached timestamp.
  useEffect(() => {
    if (!state.needsPairing) return;
    authTransport.clear();
    clearLastAuthOkAt();
  }, [state.needsPairing]);

  const value = useMemo<AppContextValue>(
    () => ({ state, dispatch, api, clientVersion: CLIENT_VERSION, goBack }),
    [state, api, goBack],
  );

  return <AppContext.Provider value={value}>{props.children}</AppContext.Provider>;
}
