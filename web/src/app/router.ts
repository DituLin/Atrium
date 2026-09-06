/**
 * Route state machine (design §7.1). There is deliberately no `react-router`:
 * the whole surface is five known routes driven by the remote and by server
 * commands, so a pure reducer over a whitelist is smaller, testable, and cannot
 * be pushed into an unknown state by a malformed command.
 *
 * Whitelist: `pair | connect | dashboard | photos/:collection | photo/:id`.
 */

import type { PhotoCollection } from '../types/api';
import { isPhotoCollection } from '../types/api';
import type { RouteState } from '../types/ws';

export type AppRoute =
  | { name: 'pair' }
  | { name: 'connect' }
  | { name: 'dashboard' }
  | { name: 'photos'; collection: PhotoCollection }
  | { name: 'photo'; photoId: string; collection?: PhotoCollection };

export const DEFAULT_ROUTE: AppRoute = { name: 'dashboard' };
export const DEFAULT_COLLECTION: PhotoCollection = 'recent';

/** ULID-ish / opaque server IDs. Kept strict so a command cannot inject a path. */
const PHOTO_ID_RE = /^[A-Za-z0-9_-]{1,64}$/;

export function isValidPhotoId(value: unknown): value is string {
  return typeof value === 'string' && PHOTO_ID_RE.test(value);
}

/**
 * Parses a path such as `photos/recent` into a route. Returns `null` for
 * anything outside the whitelist — the caller keeps the current route and (for
 * commands) acks `failed / invalid_route`.
 */
export function parseRoutePath(path: string): AppRoute | null {
  const trimmed = path.replace(/^[#/]+/, '').replace(/\/+$/, '');
  if (trimmed === '') return { name: 'dashboard' };
  const segments = trimmed.split('/');
  const head = segments[0];
  switch (head) {
    case 'pair':
      return segments.length === 1 ? { name: 'pair' } : null;
    case 'connect':
      return segments.length === 1 ? { name: 'connect' } : null;
    case 'dashboard':
      return segments.length === 1 ? { name: 'dashboard' } : null;
    case 'photos': {
      if (segments.length === 1) return { name: 'photos', collection: DEFAULT_COLLECTION };
      if (segments.length !== 2) return null;
      const collection = segments[1];
      return isPhotoCollection(collection) ? { name: 'photos', collection } : null;
    }
    case 'photo': {
      if (segments.length !== 2) return null;
      const photoId = segments[1];
      return isValidPhotoId(photoId) ? { name: 'photo', photoId } : null;
    }
    default:
      return null;
  }
}

export function routeToPath(route: AppRoute): string {
  switch (route.name) {
    case 'photos':
      return `photos/${route.collection}`;
    case 'photo':
      return `photo/${route.photoId}`;
    default:
      return route.name;
  }
}

/** The `RouteState` shape reported over WS (§9). */
export function toRouteState(route: AppRoute): RouteState {
  switch (route.name) {
    case 'photos':
      return { name: 'photos', collection: route.collection };
    case 'photo':
      return route.collection
        ? { name: 'photo', photo_id: route.photoId, collection: route.collection }
        : { name: 'photo', photo_id: route.photoId };
    default:
      return { name: route.name };
  }
}

export function routesEqual(a: AppRoute, b: AppRoute): boolean {
  return routeToPath(a) === routeToPath(b);
}

/**
 * Back key target (design §7.1: Back always leads home). A photo opened from a
 * collection returns to that collection; one opened from the dashboard (Enter
 * on the slideshow, or a `show` command) carries no collection and returns
 * straight to the dashboard, which is the route it came from.
 */
export function backRoute(route: AppRoute): AppRoute {
  switch (route.name) {
    case 'photo':
      return route.collection
        ? { name: 'photos', collection: route.collection }
        : { name: 'dashboard' };
    case 'photos':
      return { name: 'dashboard' };
    default:
      return route;
  }
}

/* ---------------------------------------------------------------- reducer */

export interface RouterState {
  route: AppRoute;
  /** Highest command sequence this client has applied (§6.5). */
  appliedSequence: number;
  /** Command IDs already handled; duplicates are ignored (§6.5). */
  handledCommandIds: readonly string[];
}

export const initialRouterState: RouterState = {
  route: DEFAULT_ROUTE,
  appliedSequence: 0,
  handledCommandIds: [],
};

/** Bounded so a long-running screen cannot grow this without limit. */
const HANDLED_COMMAND_LIMIT = 64;

export type RouterAction =
  | { type: 'router.navigate'; route: AppRoute }
  | { type: 'router.back' }
  /**
   * A command whose target is on screen but not yet rendered. The route moves
   * and the id is remembered (duplicate delivery is idempotent, §6.5), but
   * `appliedSequence` is deliberately NOT advanced: §6.5 says the client
   * applies a sequence only when it acks `applied`, and a `show` can still end
   * in `failed / photo_unavailable`.
   */
  | { type: 'router.commandStarted'; route: AppRoute; commandId: string }
  | { type: 'router.commandApplied'; route: AppRoute; sequence: number; commandId: string }
  | { type: 'router.commandRejected'; sequence: number; commandId: string }
  | { type: 'router.reset' };

function rememberCommand(state: RouterState, commandId: string): readonly string[] {
  const next = [...state.handledCommandIds, commandId];
  return next.length > HANDLED_COMMAND_LIMIT
    ? next.slice(next.length - HANDLED_COMMAND_LIMIT)
    : next;
}

export function routerReducer(state: RouterState, action: RouterAction): RouterState {
  switch (action.type) {
    case 'router.navigate':
      return routesEqual(state.route, action.route) ? state : { ...state, route: action.route };
    case 'router.back': {
      const target = backRoute(state.route);
      return routesEqual(state.route, target) ? state : { ...state, route: target };
    }
    case 'router.commandStarted':
      return {
        ...state,
        route: action.route,
        handledCommandIds: rememberCommand(state, action.commandId),
      };
    case 'router.commandApplied':
      return {
        route: action.route,
        appliedSequence: Math.max(state.appliedSequence, action.sequence),
        handledCommandIds: rememberCommand(state, action.commandId),
      };
    case 'router.commandRejected':
      return { ...state, handledCommandIds: rememberCommand(state, action.commandId) };
    case 'router.reset':
      return initialRouterState;
    default:
      return state;
  }
}

export function hasHandledCommand(state: RouterState, commandId: string): boolean {
  return state.handledCommandIds.includes(commandId);
}
