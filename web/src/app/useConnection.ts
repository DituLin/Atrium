/**
 * React wiring for the WS session (W-301). Everything with rules in it lives in
 * `session.ts` (meaning), `commandExecutor.ts` (§6.5), `refetchers.ts` (re-reads)
 * and `snapshotSync.ts` (`/home` before the socket); this hook only owns the
 * lifetime of those objects and the state mirror the callbacks read from.
 */

import { useEffect, useMemo, useRef } from 'react';

import type { ApiClient } from '../core/api';
import type { WsClient } from '../core/ws';
import { WS_PATH, createWsClient, wsUrl } from '../core/ws';
import type { CommandAckPayload, DataChangedTopic, RouteState } from '../types/ws';
import { createTopicRefetcher } from './dataChanged';
import { createRefetchers, toRefetchHandlers } from './refetchers';
import type { Session } from './session';
import { createSession } from './session';
import { syncSnapshot } from './snapshotSync';
import type { AppAction, AppState } from './state';
import { slideshowActive } from './state';
import { toRouteState } from './router';

export { DATA_CHANGED_THROTTLE_MS } from './dataChanged';

export interface ConnectionDeps {
  api: ApiClient;
  clientVersion: string;
  state: AppState;
  dispatch: (action: AppAction) => void;
  enabled: boolean;
}

/** Stable handle: the socket underneath is replaced on every reconnect. */
export interface WsHandle {
  sendState(route: RouteState): void;
  sendAck(ack: CommandAckPayload): void;
}

export function useConnection(deps: ConnectionDeps): WsHandle {
  const { api, clientVersion, dispatch, enabled } = deps;
  // The socket callbacks must read the freshest state without re-creating the
  // client on every render, so state is mirrored through a ref after commit.
  const stateRef = useRef(deps.state);
  useEffect(() => {
    stateRef.current = deps.state;
  }, [deps.state]);
  const clientRef = useRef<WsClient | null>(null);
  const sessionRef = useRef<Session | null>(null);

  // A `show` command is acked only once its image has decoded (§7.2); the
  // viewer's render signal is fed to the executor from here.
  const renderedId = deps.state.viewer.renderedId;
  const viewerStatus = deps.state.viewer.status;
  useEffect(() => {
    sessionRef.current?.settleRender(renderedId, viewerStatus);
  }, [renderedId, viewerStatus]);

  // A manual retry after `4003 superseded` rebuilds the client (W-303).
  const retryNonce = deps.state.connection.retryNonce;

  useEffect(() => {
    if (!enabled) return;

    const getState = (): AppState => stateRef.current;
    const refetchers = createRefetchers({ api, dispatch, getState });
    // Change notifications are throttled per target (W-205); the refetcher
    // lives with the socket so it is torn down with it.
    const refetch = createTopicRefetcher(toRefetchHandlers(refetchers));
    const notifyChanged = (topics: readonly DataChangedTopic[]): void => {
      refetch.notify(topics, {
        activeCollection: getState().collection.collection,
        slideshowActive: slideshowActive(getState()),
      });
    };

    const session = createSession({
      getState,
      dispatch,
      sendAck: (ack) => clientRef.current?.sendAck(ack),
      sendState: (route) => clientRef.current?.sendState(route),
      notifyChanged,
      refreshRoute: refetchers.route,
    });
    sessionRef.current = session;

    const client = createWsClient({
      url: () => wsUrl(window.location, WS_PATH),
      clientVersion,
      getRoute: () => toRouteStateOf(getState()),
      getAppliedSequence: () => getState().router.appliedSequence,
      preflight: () => syncSnapshot({ api, dispatch, getState }),
      onMessage: session.handleMessage,
      onPreflightError: (_error, status) => {
        if (status === 401) dispatch({ type: 'app.needsPairing' });
        else dispatch({ type: 'connection.attemptFailed' });
      },
      onClosed: (code, action) => {
        dispatch({ type: 'connection.closed', code, action });
        if (action === 'pair' || action === 'clear-and-pair') {
          dispatch({ type: 'app.needsPairing' });
        }
      },
    });

    clientRef.current = client;
    dispatch({ type: 'connection.start' });
    client.start();
    return () => {
      client.stop();
      clientRef.current = null;
      sessionRef.current = null;
      refetch.cancel();
    };
  }, [api, clientVersion, dispatch, enabled, retryNonce]);

  return useMemo<WsHandle>(
    () => ({
      sendState: (route) => clientRef.current?.sendState(route),
      sendAck: (ack) => clientRef.current?.sendAck(ack),
    }),
    [],
  );
}

function toRouteStateOf(state: AppState): RouteState {
  return toRouteState(state.router.route);
}
