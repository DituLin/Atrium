/**
 * WS session semantics (W-301, W-303; design §6.5, §7.2, §9).
 *
 * Everything the socket says is turned into dispatches here, with no React and
 * no timers of its own — `core/ws.ts` owns the transport (heartbeat cadence,
 * backoff, close codes) and this owns the meaning:
 *
 *   session.ready      → clock sync, connection online, report route, apply a
 *                        `pending_command` through the executor
 *   heartbeat.ack      → clock offset
 *   screen.command     → executor (W-302)
 *   data.changed       → throttled refetch mapping (W-205 / W-303)
 *   session.revoked    → clear everything and go to `pair`
 *   session.superseded → stop reconnecting, connect screen explains it
 */

import type {
  CommandAckPayload,
  DataChangedTopic,
  RouteState,
  ServerMessage,
} from '../types/ws';
import type { CommandExecutor } from './commandExecutor';
import { createCommandExecutor } from './commandExecutor';
import type { PhotoViewerStatus } from './photoViewer';
import type { AppRoute } from './router';
import { toRouteState } from './router';
import type { AppAction, AppState } from './state';

export interface SessionPorts {
  getState: () => AppState;
  dispatch: (action: AppAction) => void;
  sendAck: (ack: CommandAckPayload) => void;
  sendState: (route: RouteState) => void;
  /** Throttled `data.changed` fan-out (per target, W-205). */
  notifyChanged: (topics: readonly DataChangedTopic[]) => void;
  /** Awaited re-read of a route's data, used by the `refresh` command. */
  refreshRoute: (route: AppRoute) => Promise<void>;
  now?: () => number;
}

export interface Session {
  handleMessage: (message: ServerMessage) => void;
  /** Feed the viewer's render signal so a held `show` ack can settle. */
  settleRender: (renderedId: string | null, viewerStatus: PhotoViewerStatus) => void;
  executor: CommandExecutor;
}

export function createSession(ports: SessionPorts): Session {
  const now = ports.now ?? ((): number => Date.now());

  const executor = createCommandExecutor({
    getRouter: () => ports.getState().router,
    dispatch: ports.dispatch,
    sendAck: ports.sendAck,
    refreshRoute: ports.refreshRoute,
  });

  const syncClock = (serverTime: string): void => {
    ports.dispatch({ type: 'app.serverTime', serverTime, receivedAt: now() });
  };

  const handleMessage = (message: ServerMessage): void => {
    switch (message.type) {
      case 'session.ready': {
        syncClock(message.payload.server_time);
        ports.dispatch({ type: 'connection.socketOpen' });
        // W-304: the first thing a restored session reports is the page it is
        // actually on. The route was already re-validated by the preflight.
        ports.sendState(toRouteState(ports.getState().router.route));
        const pending = message.payload.pending_command;
        if (pending) executor.execute(pending);
        return;
      }
      case 'heartbeat.ack':
        syncClock(message.payload.server_time);
        return;
      case 'data.changed':
        ports.notifyChanged(message.payload.topics);
        return;
      case 'screen.command':
        executor.execute(message.payload);
        return;
      case 'session.revoked':
        // PRD 8.2 / design §6.5: forget the screen entirely. `app.needsPairing`
        // purges photos and the snapshot, the provider clears the credential
        // and `lastAuthOkAt`, and the route must not keep pointing at content.
        ports.dispatch({ type: 'app.needsPairing' });
        ports.dispatch({ type: 'router.reset' });
        ports.dispatch({ type: 'router.navigate', route: { name: 'pair' } });
        return;
      case 'session.superseded':
        // Ahead of the `4003` close, so the connect screen can explain itself
        // even if the close frame never arrives.
        ports.dispatch({ type: 'connection.superseded' });
        return;
      default:
        return;
    }
  };

  return {
    handleMessage,
    settleRender: (renderedId, viewerStatus) => executor.settleRender(renderedId, viewerStatus),
    executor,
  };
}
