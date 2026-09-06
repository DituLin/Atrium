/**
 * Client-side command rules (design §6.5) — the pure decisions only. The
 * stateful side (dispatching, holding a `show` ack until the image decoded,
 * awaiting a `refresh`) lives in `commandExecutor.ts`.
 *
 *  - apply only if `sequence > applied_sequence`, otherwise ack `superseded`
 *  - a duplicate `command_id` is ignored entirely (idempotent delivery)
 *  - the client never evaluates `expires_at` with its own clock
 *  - an unparseable payload acks `failed / invalid_payload | invalid_route`
 */

import { isPhotoCollection } from '../types/api';
import type { CommandAckPayload, ScreenCommand } from '../types/ws';
import type { AppRoute, RouterState } from './router';
import { DEFAULT_COLLECTION, hasHandledCommand, isValidPhotoId, toRouteState } from './router';

export type CommandDecision =
  | { kind: 'ignore'; reason: 'duplicate' }
  | { kind: 'apply'; route: AppRoute; command: ScreenCommand; resourceId?: string }
  | { kind: 'refresh'; command: ScreenCommand }
  | { kind: 'reject'; ack: CommandAckPayload };

function reject(
  command: ScreenCommand,
  state: RouterState,
  errorCode: CommandAckPayload['error_code'],
): CommandDecision {
  return {
    kind: 'reject',
    ack: {
      command_id: command.command_id,
      status: 'failed',
      route: toRouteState(state.route),
      error_code: errorCode,
    },
  };
}

function parseNavigate(payload: unknown): AppRoute | null {
  if (typeof payload !== 'object' || payload === null) return null;
  const record = payload as { route?: unknown; collection?: unknown };
  if (record.route === 'dashboard') return { name: 'dashboard' };
  if (record.route === 'photos') {
    if (record.collection === undefined || record.collection === null) {
      return { name: 'photos', collection: DEFAULT_COLLECTION };
    }
    return isPhotoCollection(record.collection)
      ? { name: 'photos', collection: record.collection }
      : null;
  }
  return null;
}

function parseShow(payload: unknown): string | null {
  if (typeof payload !== 'object' || payload === null) return null;
  const record = payload as { photo_id?: unknown };
  return isValidPhotoId(record.photo_id) ? record.photo_id : null;
}

/**
 * Decides what to do with an inbound `screen.command`. Pure: the caller applies
 * the route change, waits for the render, and then sends the `applied` ack.
 */
export function decideCommand(state: RouterState, command: ScreenCommand): CommandDecision {
  if (hasHandledCommand(state, command.command_id)) {
    return { kind: 'ignore', reason: 'duplicate' };
  }
  if (!(command.sequence > state.appliedSequence)) {
    return reject(command, state, 'superseded');
  }
  switch (command.kind) {
    case 'navigate': {
      const route = parseNavigate(command.payload);
      return route ? { kind: 'apply', route, command } : reject(command, state, 'invalid_route');
    }
    case 'show': {
      const photoId = parseShow(command.payload);
      if (!photoId) return reject(command, state, 'invalid_payload');
      const collection = state.route.name === 'photos' ? state.route.collection : undefined;
      const route: AppRoute = collection
        ? { name: 'photo', photoId, collection }
        : { name: 'photo', photoId };
      return { kind: 'apply', route, command, resourceId: photoId };
    }
    case 'refresh':
      return { kind: 'refresh', command };
    default:
      return reject(command, state, 'invalid_payload');
  }
}

/** The ack sent once the target route has actually rendered. */
export function appliedAck(
  command: ScreenCommand,
  route: AppRoute,
  resourceId?: string,
): CommandAckPayload {
  const ack: CommandAckPayload = {
    command_id: command.command_id,
    status: 'applied',
    route: toRouteState(route),
  };
  return resourceId ? { ...ack, resource_id: resourceId } : ack;
}

/* --------------------------------------------------- render-confirmed acks */

/**
 * §7.2 wants the ack sent only after the target screen has actually rendered.
 * For `navigate` the route change is the render (the next paint shows it), but
 * `show` must wait for the image itself: the viewer reports `<img onload>` as
 * `viewer.rendered`, and only then is the command applied (W-302).
 */
export function needsRenderConfirmation(route: AppRoute): boolean {
  return route.name === 'photo';
}

export interface PendingAck {
  command: ScreenCommand;
  route: AppRoute;
  resourceId: string;
}

/** The ack to hold back, or `null` when the decision can be acked at once. */
export function pendingAckFor(decision: CommandDecision): PendingAck | null {
  if (decision.kind !== 'apply') return null;
  if (!needsRenderConfirmation(decision.route) || !decision.resourceId) return null;
  return { command: decision.command, route: decision.route, resourceId: decision.resourceId };
}

/**
 * Resolves a held ack against the render signal. Returns the ack to send, or
 * `null` while the image has not been decoded yet.
 */
export function resolvePendingAck(
  pending: PendingAck | null,
  renderedPhotoId: string | null,
): CommandAckPayload | null {
  if (!pending || renderedPhotoId === null) return null;
  if (pending.resourceId !== renderedPhotoId) return null;
  return appliedAck(pending.command, pending.route, pending.resourceId);
}

/** The ack for a `show` whose photo turned out to be unusable (404 / expired). */
export function failedAck(
  pending: PendingAck,
  errorCode: CommandAckPayload['error_code'] = 'photo_unavailable',
): CommandAckPayload {
  return {
    command_id: pending.command.command_id,
    status: 'failed',
    route: toRouteState(pending.route),
    resource_id: pending.resourceId,
    error_code: errorCode,
  };
}
