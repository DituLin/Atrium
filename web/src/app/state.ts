/**
 * The single application state tree and its reducer.
 *
 * Everything here is pure: sub-reducers for router, connection and pairing plus
 * the small amount of glue (home snapshot, clock, auth-cache expiry). The React
 * layer only dispatches; all rules are testable without rendering.
 */

import type { ClockState } from '../core/clock';
import { clockReducer, initialClockState } from '../core/clock';
import type { HomeResponse, PhotoWidgetPayload } from '../types/api';
import type { ConnectionAction, ConnectionState } from './connection';
import { connectionReducer, initialConnectionState, needsConnectScreen } from './connection';
import type { PairingAction, PairingState } from './pairing';
import { initialPairingState, pairingReducer } from './pairing';
import type { PhotoListAction, PhotoListState } from './photoList';
import { initialPhotoListState, photoListReducer } from './photoList';
import type { PhotoViewerAction, PhotoViewerState } from './photoViewer';
import { initialPhotoViewerState, photoViewerReducer } from './photoViewer';
import type { AppRoute, RouterAction, RouterState } from './router';
import { initialRouterState, routerReducer } from './router';
import type { SlideshowAction, SlideshowState } from './slideshow';
import { initialSlideshowState, slideshowReducer } from './slideshow';

export type ScreenName = 'pair' | 'connect' | 'dashboard' | 'photos' | 'photo';

export interface AppState {
  router: RouterState;
  connection: ConnectionState;
  pairing: PairingState;
  clock: ClockState;
  /** Latest `/api/v1/home` snapshot, or `null` when nothing is cached. */
  home: HomeResponse | null;
  /** Dashboard slideshow: one seeded `random` round (W-201). */
  slideshow: SlideshowState;
  /** The collection browser's page, focus and cursor (W-202). */
  collection: PhotoListState;
  /** The single-photo viewer (W-203). */
  viewer: PhotoViewerState;
  /** True while the 24 h authorization cache rule blocks rendering (PRD 8.2). */
  authExpired: boolean;
  /** Set by a 401 or a `4002 revoked` close: the screen must pair again. */
  needsPairing: boolean;
  /** Client epoch, ticked once per second so the clock re-renders. */
  nowMs: number;
}

export function createInitialState(nowMs: number): AppState {
  return {
    router: initialRouterState,
    connection: initialConnectionState,
    pairing: initialPairingState,
    clock: initialClockState,
    home: null,
    slideshow: initialSlideshowState,
    collection: initialPhotoListState,
    viewer: initialPhotoViewerState,
    authExpired: false,
    needsPairing: false,
    nowMs,
  };
}

export type AppAction =
  | RouterAction
  | ConnectionAction
  | PairingAction
  | SlideshowAction
  | PhotoListAction
  | PhotoViewerAction
  | { type: 'app.tick'; nowMs: number }
  | { type: 'app.homeLoaded'; home: HomeResponse; receivedAt: number }
  | { type: 'app.serverTime'; serverTime: string; receivedAt: number }
  | { type: 'app.authOk' }
  | { type: 'app.authExpired' }
  | { type: 'app.needsPairing' };

const ROUTER_PREFIX = 'router.';
const CONNECTION_PREFIX = 'connection.';
const PAIR_PREFIX = 'pair.';
const SLIDESHOW_PREFIX = 'slideshow.';
const PHOTO_LIST_PREFIX = 'photos.';
const VIEWER_PREFIX = 'viewer.';

/** Everything holding family content, cleared together (PRD 8.2). */
function purgedPhotoState(state: AppState): AppState {
  return {
    ...state,
    slideshow: slideshowReducer(state.slideshow, { type: 'slideshow.reset' }),
    collection: photoListReducer(state.collection, { type: 'photos.reset' }),
    viewer: photoViewerReducer(state.viewer, { type: 'viewer.close' }),
    home: null,
  };
}

export function appReducer(state: AppState, action: AppAction): AppState {
  if (action.type.startsWith(ROUTER_PREFIX)) {
    const router = routerReducer(state.router, action as RouterAction);
    return router === state.router ? state : { ...state, router };
  }
  if (action.type.startsWith(CONNECTION_PREFIX)) {
    const connection = connectionReducer(state.connection, action as ConnectionAction);
    return connection === state.connection ? state : { ...state, connection };
  }
  if (action.type.startsWith(SLIDESHOW_PREFIX)) {
    if (state.authExpired) return state;
    const slideshow = slideshowReducer(state.slideshow, action as SlideshowAction);
    return slideshow === state.slideshow ? state : { ...state, slideshow };
  }
  if (action.type.startsWith(PHOTO_LIST_PREFIX)) {
    if (state.authExpired) return state;
    const collection = photoListReducer(state.collection, action as PhotoListAction);
    return collection === state.collection ? state : { ...state, collection };
  }
  if (action.type.startsWith(VIEWER_PREFIX)) {
    if (state.authExpired) return state;
    const viewer = photoViewerReducer(state.viewer, action as PhotoViewerAction);
    return viewer === state.viewer ? state : { ...state, viewer };
  }
  if (action.type.startsWith(PAIR_PREFIX)) {
    const pairing = pairingReducer(state.pairing, action as PairingAction);
    const next = pairing === state.pairing ? state : { ...state, pairing };
    // A successful claim ends the "needs pairing" condition.
    return action.type === 'pair.claimed' ? { ...next, needsPairing: false } : next;
  }

  switch (action.type) {
    case 'app.tick':
      return { ...state, nowMs: action.nowMs };
    case 'app.homeLoaded': {
      const interval = slideshowIntervalSeconds(action.home);
      return {
        ...state,
        home: action.home,
        clock: clockReducer(state.clock, {
          type: 'clock.sync',
          serverTime: action.home.server_time,
          receivedAt: action.receivedAt,
        }),
        slideshow:
          interval === null
            ? state.slideshow
            : slideshowReducer(state.slideshow, {
                type: 'slideshow.configure',
                intervalSeconds: interval,
              }),
        authExpired: false,
        needsPairing: false,
      };
    }
    case 'app.serverTime':
      return {
        ...state,
        clock: clockReducer(state.clock, {
          type: 'clock.sync',
          serverTime: action.serverTime,
          receivedAt: action.receivedAt,
        }),
      };
    case 'app.authOk':
      return state.authExpired || state.needsPairing
        ? { ...state, authExpired: false, needsPairing: false }
        : state;
    case 'app.authExpired':
      // PRD 8.2: purge photo state and stop rendering cached family content.
      return state.authExpired ? state : { ...purgedPhotoState(state), authExpired: true };
    case 'app.needsPairing':
      return { ...purgedPhotoState(state), needsPairing: true };
    default:
      return state;
  }
}

/**
 * Which screen to render. Pairing and the auth-cache rule outrank everything;
 * after that the connection state machine decides between the connect screen
 * and the current route.
 */
export function selectScreen(state: AppState): ScreenName {
  if (state.needsPairing) return 'pair';
  if (state.router.route.name === 'pair') return 'pair';
  if (state.authExpired) return 'connect';
  if (needsConnectScreen(state.connection)) return 'connect';
  return state.router.route.name;
}

export function selectRoute(state: AppState): AppRoute {
  return state.router.route;
}

/** `photo.slideshow_interval_seconds` from the snapshot, when it is present. */
export function slideshowIntervalSeconds(home: HomeResponse): number | null {
  for (const widget of home.widgets) {
    if (widget.type !== 'photo') continue;
    const payload = widget.payload as Partial<PhotoWidgetPayload>;
    const seconds = payload.slideshow_interval_seconds;
    if (typeof seconds === 'number') return seconds;
  }
  return null;
}

/** The slideshow only runs on the dashboard (design §7.2, PRD 5.3). */
export function slideshowActive(state: AppState): boolean {
  return selectScreen(state) === 'dashboard' && !state.slideshow.paused;
}
