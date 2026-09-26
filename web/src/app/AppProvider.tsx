import { VideoRefresh } from './videoRefresh';
/**
 * Composition root: builds the API client, runs the reducer, and wires the
 * three cross-cutting rules — the 24 h auth cache (PRD 8.2), the one-second
 * clock tick, and the remote Back key.
 */

import { createOverviewLoader } from './overview';
import { useOverviewLifecycle } from './useOverviewLifecycle';


import type { ReactElement, ReactNode } from 'react';
import { useCallback, useEffect, useMemo, useReducer, useRef } from 'react';

import { createHouseLoader } from './house';
import { useHouseLifecycle } from './useHouseLifecycle';
import { authTransport } from '../core/auth';
import { clearLastAuthOkAt, createAuthExpiryWatcher } from '../core/authExpiry';
import { createScreenApi } from './apiClient';
import type { AppContextValue } from './context';
import { AppContext } from './context';
import { toRouteState } from './router';
import type { AppAction } from './state';
import { appReducer, createInitialState, selectScreen } from './state';
import { useConnection } from './useConnection';
import { useRemoteKeys, useSecondTick } from './useTick';

export const CLIENT_VERSION: string =
  typeof __CLIENT_VERSION__ === 'string' ? __CLIENT_VERSION__ : 'dev';

export function AppProvider(props: { children: ReactNode }): ReactElement {
  const [state, reduce] = useReducer(appReducer, 0, createInitialState);
  const { api, dispatch, house, overview } = useMemo(() => {
    const send = (action: AppAction) => {
      if (action.type === 'app.needsPairing' || action.type === 'app.authExpired' || action.type === 'pair.claimed') {
        house.invalidate(); overview.invalidate();
        client.invalidateAuthorization();
        if (action.type !== 'pair.claimed') clearLastAuthOkAt();
      }
      reduce(action);
    };
    const client = createScreenApi(send);
    const house = createHouseLoader(client, send);
    const overview = createOverviewLoader(client, send);
    return { api: client, dispatch: send, house, overview };
  }, [reduce]);
  const videos = useMemo(() => new VideoRefresh(), []);
  const stateRef = useRef(state);
  useEffect(() => {
    stateRef.current = state;
  }, [state]);

  useEffect(() => () => { api.invalidateAuthorization(); house.invalidate(); overview.invalidate(); }, [api, house, overview]);

  // Seed the clock immediately; the one-second tick keeps it moving.
  useEffect(() => {
    dispatch({ type: 'app.tick', nowMs: Date.now() });
  }, [dispatch]);

  // PRD 8.2 — boot, visibilitychange and a 60 s timer all funnel through here.
  useEffect(() => {
    const watcher = createAuthExpiryWatcher({
      onExpired: () => {
        if (!stateRef.current.needsPairing) dispatch({ type: 'app.authExpired' });
      },
    });
    watcher.start();
    return () => watcher.stop();
  }, [dispatch]);

  useSecondTick(useCallback((nowMs: number) => dispatch({ type: 'app.tick', nowMs }), [dispatch]));

  const goBack = useCallback(() => dispatch({ type: 'router.back' }), [dispatch]);

  useRemoteKeys(
    useCallback(
      (key) => {
        if (key !== 'back') return false;
        const screen = selectScreen(stateRef.current);
        if (screen === 'dashboard') {
          const hero = document.querySelector<HTMLElement>('[data-home-hero]');
          if (!hero || document.activeElement === hero) return false;
          hero.focus(); return true;
        }
        if (screen === 'pair' || screen === 'connect') return false;
        goBack();
        return true;
      },
      [goBack],
    ),
  );

  // The socket runs only when the screen believes it holds a credential and the
  // auth cache has not expired. Pairing screens never open one.
  const connectionEnabled =
    !state.needsPairing && !state.authExpired && authTransport.hasCredential();
  const ws = useConnection({ api, clientVersion: CLIENT_VERSION, state, dispatch, house, overview, videos, enabled: connectionEnabled });

  // Home shows the first family notice, so the overview also polls there.
  const overviewScreen = selectScreen(state);
  useOverviewLifecycle(overview, overviewScreen === 'briefing' || overviewScreen === 'dashboard', state.connection.status);
  useHouseLifecycle(house, selectScreen(state) === 'house', state.connection.status);

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
    () => ({ state, dispatch, api, house, overview, videos, clientVersion: CLIENT_VERSION, goBack }),
    [state, api, house, overview, videos, goBack, dispatch],
  );

  return <AppContext.Provider value={value}>{props.children}</AppContext.Provider>;
}
