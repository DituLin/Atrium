/**
 * The preflight that runs before every socket open (W-304, design §7.2:
 * "each attempt first calls `GET /api/v1/home` (auth + snapshot) then opens the
 * WS"). It is also where the current page is re-validated against the fresh
 * snapshot, so the `state` the session reports on `session.ready` is a page the
 * screen can really render.
 *
 * Rejecting propagates the `ApiError` (status 401 routes to pairing).
 */

import { ApiError } from '../core/api';
import type { ApiClient } from '../core/api';
import type { PhotoAvailability } from './routeSync';
import { isColdStart, photoToRevalidate, routeAfterReconnect } from './routeSync';
import { routesEqual } from './router';
import type { AppAction, AppState } from './state';

export interface SnapshotSyncPorts {
  api: ApiClient;
  dispatch: (action: AppAction) => void;
  getState: () => AppState;
  now?: () => number;
}

/** Is this photo still served by the Core? Transport errors stay `unknown`. */
export async function checkPhoto(api: ApiClient, photoId: string): Promise<PhotoAvailability> {
  try {
    await api.getPhoto(photoId);
    return 'present';
  } catch (error) {
    if (error instanceof ApiError && error.isGone) return 'gone';
    return 'unknown';
  }
}

export async function syncSnapshot(ports: SnapshotSyncPorts): Promise<void> {
  const now = ports.now ?? ((): number => Date.now());
  // Read before dispatching: the state seen by the reducer is only visible to
  // this closure after the next commit.
  const before = ports.getState();
  const cold = isColdStart({
    hasSnapshot: before.connection.hasSnapshot,
    appliedSequence: before.router.appliedSequence,
  });

  const home = await ports.api.getHome();
  ports.dispatch({ type: 'app.homeLoaded', home, receivedAt: now() });
  ports.dispatch({ type: 'connection.snapshotOk' });

  // A cold start has nothing to restore, so nothing is re-validated: the boot
  // route is `coldStartRoute()` by construction (PRD 5.3).
  const photoId = cold ? null : photoToRevalidate(before.router.route);
  const availability = photoId ? await checkPhoto(ports.api, photoId) : 'present';
  const next = routeAfterReconnect(before.router.route, availability);

  // The screen may have moved on while `/home` and the photo check were in
  // flight; a reconnect must never stomp on a newer local navigation.
  const current = ports.getState().router.route;
  if (routesEqual(current, before.router.route) && !routesEqual(current, next)) {
    ports.dispatch({ type: 'router.navigate', route: next });
  }
}
