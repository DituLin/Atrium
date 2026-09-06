/**
 * The re-read side of the session: the same fetches are needed by the throttled
 * `data.changed` path (W-205/W-303) and by the `refresh` command (W-302), the
 * difference being that `refresh` must be awaited so its ack lands after the
 * data actually came back.
 *
 * Nothing here touches the route or the slideshow's paused flag: `refresh`
 * keeps the current page and pause state (PRD 5.3).
 */

import type { ApiClient } from '../core/api';
import { COLLECTION_PAGE_SIZE } from './photoList';
import type { AppRoute } from './router';
import { SLIDESHOW_PAGE_SIZE } from './slideshow';
import type { AppAction, AppState } from './state';

export interface RefetchPorts {
  api: ApiClient;
  dispatch: (action: AppAction) => void;
  getState: () => AppState;
}

export interface Refetchers {
  home(): Promise<void>;
  collection(): Promise<void>;
  slideshow(): Promise<void>;
  /** Everything the given route displays; used by the `refresh` command. */
  route(route: AppRoute): Promise<void>;
}

export function createRefetchers(ports: RefetchPorts): Refetchers {
  const { api, dispatch, getState } = ports;

  const home = async (): Promise<void> => {
    const snapshot = await api.getHome();
    dispatch({ type: 'app.homeLoaded', home: snapshot, receivedAt: Date.now() });
  };

  const collection = async (): Promise<void> => {
    const list = getState().collection;
    const name = list.collection;
    if (!name) return;
    const page = await api.listPhotos({ collection: name, limit: COLLECTION_PAGE_SIZE });
    dispatch({
      type: 'photos.pageLoaded',
      collection: name,
      generation: list.generation,
      items: page.items,
      nextCursor: page.next_cursor,
      meta: page.meta ?? null,
      append: false,
    });
  };

  const slideshow = async (): Promise<void> => {
    const seed = getState().slideshow.seed;
    if (!seed) return;
    const page = await api.listPhotos({ collection: 'random', seed, limit: SLIDESHOW_PAGE_SIZE });
    // Replaces the round in place: the visible photo keeps its slot and its
    // decoded image (W-205).
    dispatch({
      type: 'slideshow.listRefreshed',
      items: page.items,
      nextCursor: page.next_cursor,
    });
  };

  const route = async (target: AppRoute): Promise<void> => {
    const jobs: Array<Promise<void>> = [home()];
    switch (target.name) {
      case 'dashboard':
        if (getState().slideshow.seed) jobs.push(slideshow());
        break;
      case 'photos':
        jobs.push(collection());
        break;
      case 'photo':
        // The image on screen is left alone; the list behind it is re-read so
        // Left/Right keep walking fresh neighbours.
        if (target.collection) jobs.push(collection());
        break;
      default:
        break;
    }
    await Promise.all(jobs);
  };

  return { home, collection, slideshow, route };
}

/** Fire-and-forget wrappers for the throttled `data.changed` path. */
export function toRefetchHandlers(refetchers: Refetchers): {
  home: () => void;
  collection: () => void;
  slideshow: () => void;
} {
  const swallow = (run: () => Promise<void>) => (): void => {
    // The connection machine already reports transport failures; a failed
    // refresh simply keeps whatever is on screen.
    void run().catch(() => undefined);
  };
  return {
    home: swallow(refetchers.home),
    collection: swallow(refetchers.collection),
    slideshow: swallow(refetchers.slideshow),
  };
}
