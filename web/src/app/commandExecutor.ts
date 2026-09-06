/**
 * The command executor (W-302, design §6.5 / §7.2).
 *
 * `commands.ts` holds the pure decisions; this is the small stateful part that
 * turns a decision into dispatches and acks, and holds a `show` ack back until
 * the image has actually decoded.
 *
 * Invariants:
 *  - `appliedSequence` moves only when an `applied` ack is sent, so a `show`
 *    that ends in `photo_unavailable` never blocks a later command;
 *  - every command id that reached a decision is remembered, so a re-delivery
 *    is ignored rather than executed or acked twice;
 *  - `expires_at` is never compared against the local clock (the server owns
 *    expiry); the client only ever answers what it did.
 */

import type { CommandAckPayload, ScreenCommand } from '../types/ws';
import { appliedAck, decideCommand, failedAck, pendingAckFor, resolvePendingAck } from './commands';
import type { PendingAck } from './commands';
import type { PhotoViewerStatus } from './photoViewer';
import type { AppRoute, RouterState } from './router';
import type { AppAction } from './state';

export interface ExecutorPorts {
  getRouter: () => RouterState;
  dispatch: (action: AppAction) => void;
  sendAck: (ack: CommandAckPayload) => void;
  /** Re-reads the data behind a route; resolves when the screen is current. */
  refreshRoute: (route: AppRoute) => Promise<void>;
}

export interface CommandExecutor {
  execute: (command: ScreenCommand) => void;
  /** Render signal from the photo viewer; settles a held `show` ack. */
  settleRender: (renderedId: string | null, viewerStatus: PhotoViewerStatus) => void;
  pending: () => PendingAck | null;
}

export function createCommandExecutor(ports: ExecutorPorts): CommandExecutor {
  const { getRouter, dispatch, sendAck, refreshRoute } = ports;
  let held: PendingAck | null = null;

  const applyNow = (command: ScreenCommand, route: AppRoute, resourceId?: string): void => {
    dispatch({
      type: 'router.commandApplied',
      route,
      sequence: command.sequence,
      commandId: command.command_id,
    });
    sendAck(appliedAck(command, route, resourceId));
  };

  const runRefresh = (command: ScreenCommand, route: AppRoute): void => {
    // The route and the slideshow's paused flag are untouched (PRD 5.3); only
    // the data behind the current page is re-read, and the ack waits for it.
    dispatch({ type: 'router.commandStarted', route, commandId: command.command_id });
    void refreshRoute(route).then(
      () => applyNow(command, route),
      () => {
        sendAck({
          command_id: command.command_id,
          status: 'failed',
          route: appliedAck(command, route).route,
          error_code: 'render_failed',
        });
      },
    );
  };

  return {
    execute(command) {
      const router = getRouter();
      const decision = decideCommand(router, command);
      switch (decision.kind) {
        case 'ignore':
          return;
        case 'reject':
          dispatch({
            type: 'router.commandRejected',
            sequence: command.sequence,
            commandId: command.command_id,
          });
          sendAck(decision.ack);
          return;
        case 'refresh':
          runRefresh(command, router.route);
          return;
        case 'apply': {
          // `show` pauses the slideshow until Back, another command, or a
          // manual photo change (PRD 5.3).
          if (decision.route.name === 'photo') dispatch({ type: 'slideshow.pause' });
          const pending = pendingAckFor(decision);
          if (pending) {
            held = pending;
            dispatch({
              type: 'router.commandStarted',
              route: decision.route,
              commandId: command.command_id,
            });
            return;
          }
          applyNow(command, decision.route, decision.resourceId);
          return;
        }
        default:
          return;
      }
    },

    settleRender(renderedId, viewerStatus) {
      const pending = held;
      if (!pending) return;
      const ack = resolvePendingAck(pending, renderedId);
      if (ack) {
        held = null;
        applyNow(pending.command, pending.route, pending.resourceId);
        return;
      }
      if (viewerStatus === 'missing' || viewerStatus === 'error') {
        held = null;
        sendAck(failedAck(pending));
      }
    },

    pending: () => held,
  };
}
