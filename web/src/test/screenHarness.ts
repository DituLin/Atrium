/**
 * A headless Atrium screen: the real reducer, the real session and executor and
 * the real WS transport, wired the way `AppProvider` + `useConnection` wire them
 * in the browser, but driven from Node against the mock core.
 *
 * It exists so V0.3 can be verified end to end (W-301..W-304) rather than
 * against fakes only: HTTP, the socket, the ack round trip and the photo render
 * signal are all real.
 */

import WebSocket from 'ws';

import { createSession } from '../app/session';
import type { Session } from '../app/session';
import { createRefetchers, toRefetchHandlers } from '../app/refetchers';
import { createTopicRefetcher } from '../app/dataChanged';
import { syncSnapshot } from '../app/snapshotSync';
import { appReducer, createInitialState, slideshowActive } from '../app/state';
import type { AppAction, AppState } from '../app/state';
import { toRouteState } from '../app/router';
import { ApiClient } from '../core/api';
import type { SocketLike, WsClient } from '../core/ws';
import { createWsClient } from '../core/ws';
import type { DataChangedTopic } from '../types/ws';

export interface ScreenHarness {
  state: () => AppState;
  dispatch: (action: AppAction) => void;
  api: ApiClient;
  session: Session;
  client: WsClient;
  changed: DataChangedTopic[][];
  stop: () => void;
}

export interface HarnessOptions {
  baseUrl: string;
  wsUrl: string;
  token: string;
  heartbeatIntervalMs?: number;
}

export function createScreenHarness(options: HarnessOptions): ScreenHarness {
  let state = createInitialState(Date.now());
  let client: WsClient | null = null;
  let session: Session | null = null;
  const changed: DataChangedTopic[][] = [];

  const api = new ApiClient({
    fetchImpl: (input, init) =>
      fetch(`${options.baseUrl}${input}`, {
        ...init,
        headers: { ...(init?.headers as Record<string, string>), Authorization: `Bearer ${options.token}` },
      }),
  });

  // Mirrors the two effects the provider owns: report a route change with
  // `state`, and feed the viewer's render signal to the held `show` ack.
  const dispatch = (action: AppAction): void => {
    const before = state;
    state = appReducer(state, action);
    if (state.router.route !== before.router.route) {
      client?.sendState(toRouteState(state.router.route));
    }
    if (
      state.viewer.renderedId !== before.viewer.renderedId ||
      state.viewer.status !== before.viewer.status
    ) {
      session?.settleRender(state.viewer.renderedId, state.viewer.status);
    }
  };

  const getState = (): AppState => state;
  const refetchers = createRefetchers({ api, dispatch, getState });
  const refetch = createTopicRefetcher(toRefetchHandlers(refetchers));

  session = createSession({
    getState,
    dispatch,
    sendAck: (ack) => client?.sendAck(ack),
    sendState: (route) => client?.sendState(route),
    notifyChanged: (topics) => {
      changed.push([...topics]);
      refetch.notify(topics, {
        activeCollection: getState().collection.collection,
        slideshowActive: slideshowActive(getState()),
      });
    },
    refreshRoute: refetchers.route,
  });

  client = createWsClient({
    url: () => options.wsUrl,
    clientVersion: '0.3.0+e2e',
    getRoute: () => toRouteState(getState().router.route),
    getAppliedSequence: () => getState().router.appliedSequence,
    preflight: () => syncSnapshot({ api, dispatch, getState }),
    onMessage: session.handleMessage,
    onPreflightError: (_error, status) => {
      if (status === 401) dispatch({ type: 'app.needsPairing' });
      else dispatch({ type: 'connection.attemptFailed' });
    },
    onClosed: (code, action) => {
      dispatch({ type: 'connection.closed', code, action });
      if (action === 'pair' || action === 'clear-and-pair') dispatch({ type: 'app.needsPairing' });
    },
    socketFactory: (url) =>
      new WebSocket(url, {
        headers: { Authorization: `Bearer ${options.token}` },
      }) as unknown as SocketLike,
    ...(options.heartbeatIntervalMs === undefined
      ? {}
      : { heartbeatIntervalMs: options.heartbeatIntervalMs }),
  });

  dispatch({ type: 'connection.start' });
  client.start();

  return {
    state: getState,
    dispatch,
    api,
    session,
    client,
    changed,
    stop: () => {
      client?.stop();
      refetch.cancel();
    },
  };
}
