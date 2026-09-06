/**
 * Route survival across a reconnect (W-304, PRD 5.3, design §7.2).
 *
 * "重新连接后保留并上报当前有效页面；客户端冷启动或内容失效回到 Dashboard."
 *
 * The rules are pure so the reconnect path can be tested without a socket:
 *  - a cold start (nothing in memory) has nothing to restore: the boot route
 *    is `initialRouterState.route` = dashboard, and no photo is re-validated
 *    because there is no previous page to keep;
 *  - `pair` / `connect` are transport screens, never restored as content;
 *  - `photos/:collection` survives — the collection is whitelisted and an empty
 *    one has its own empty state, so a fresh snapshot cannot invalidate it;
 *  - `photo/:id` survives only while the photo is still there; a photo the
 *    fresh snapshot no longer serves falls back to the dashboard.
 */

import type { AppRoute } from './router';
import { DEFAULT_ROUTE } from './router';

/** What the post-reconnect check learned about the route's photo. */
export type PhotoAvailability = 'present' | 'gone' | 'unknown';

/**
 * True when the client has nothing in memory to restore: no snapshot has ever
 * been rendered in this page load. A reload of the TV browser lands here.
 */
export function isColdStart(input: { hasSnapshot: boolean; appliedSequence: number }): boolean {
  return !input.hasSnapshot && input.appliedSequence === 0;
}

/** The route a cold start reports (PRD 5.3): the dashboard, always. */
export function coldStartRoute(): AppRoute {
  return DEFAULT_ROUTE;
}

/** The photo whose existence has to be re-checked, or `null` when none. */
export function photoToRevalidate(route: AppRoute): string | null {
  return route.name === 'photo' ? route.photoId : null;
}

/**
 * The route to keep after a reconnect. `photo` is the only route that can be
 * invalidated by fresh data; a check that failed for transport reasons
 * (`unknown`) keeps the photo rather than punishing a flaky moment.
 */
export function routeAfterReconnect(route: AppRoute, photo: PhotoAvailability): AppRoute {
  switch (route.name) {
    case 'pair':
    case 'connect':
      return DEFAULT_ROUTE;
    case 'photo':
      return photo === 'gone' ? DEFAULT_ROUTE : route;
    case 'photos':
    case 'dashboard':
    default:
      return route;
  }
}
