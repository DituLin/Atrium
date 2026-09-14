/**
 * The re-read side of the session: the same fetches are needed by the throttled
 * `data.changed` path (W-205/W-303) and by the `refresh` command (W-302), the
 * difference being that `refresh` must be awaited so its ack lands after the
 * data actually came back.
 *
 * Nothing here touches the route or the slideshow's paused flag: `refresh`
 * keeps the current page and pause state (PRD 5.3).
 */

import type { VideoRefresh } from './videoRefresh';
import type { OverviewLoader } from './overview';


import type { HouseLoader } from './house';
import type { ApiClient } from '../core/api';
import type { PhotoItem, PhotoListMeta } from '../types/api';
import { COLLECTION_PAGE_SIZE } from './photoList';
import type { AppRoute } from './router';
import { SLIDESHOW_PAGE_SIZE } from './slideshow';
import type { AppAction, AppState } from './state';

export interface RefetchPorts {
  videos?: VideoRefresh;
  house?: HouseLoader;
  overview?: OverviewLoader;
  api: ApiClient;
  dispatch: (action: AppAction) => void;
  getState: () => AppState;
}

export interface Refetchers {
  house(): Promise<void>;
  overview(): Promise<void>;
  home(): Promise<void>;
  collection(): Promise<void>;
  slideshow(): Promise<void>;
  /** Everything the given route displays; used by the `refresh` command. */
  route(route: AppRoute): Promise<void>;
}

export function createRefetchers(ports: RefetchPorts): Refetchers {
  const { api, dispatch, getState } = ports;

  const overview = (): Promise<void> => ports.overview?.load() ?? Promise.reject(new Error('Overview loader unavailable'));

  const house = (): Promise<void> => {
    if (!ports.house) return Promise.reject(new Error('House loader unavailable'));
    return ports.house.load();
  };

  const home = async (): Promise<void> => {
    const snapshot = await api.getHome();
    dispatch({ type: 'app.homeLoaded', home: snapshot, receivedAt: Date.now() });
  };

  const collection = async (): Promise<void> => {
    const list = getState().collection;
    const name = list.collection;
    if (!name) return;
    const windowChanged = (): boolean => {
      const latest = getState().collection;
      if (latest.generation !== list.generation) return true;
      if (latest.items !== list.items || latest.cursor !== list.cursor
        || latest.focusIndex !== list.focusIndex || latest.returnFocus !== list.returnFocus) {
        dispatch({ type: 'photos.refreshDeferred', generation: list.generation });
        return true;
      }
      return false;
    };
    // Rebuild the loaded window atomically. Replacing only page one loses a
    // paginated return card even when it still exists on the next page.
    const budget = Math.max(1, Math.ceil(list.items.length / COLLECTION_PAGE_SIZE));
    const items: PhotoItem[] = [];
    const known = new Set<string>();
    const cursors = new Set<string>();
    let cursor: string | null = null;
    let meta: PhotoListMeta | null = null;
    for (let pageIndex = 0; pageIndex < budget; pageIndex += 1) {
      if (windowChanged()) return;
      const page = await api.listPhotos({ collection: name, cursor, limit: COLLECTION_PAGE_SIZE });
      for (const item of page.items) if (!known.has(item.id)) { known.add(item.id); items.push(item); }
      meta = page.meta ?? meta;
      cursor = page.next_cursor;
      if (!cursor) break;
      if (cursors.has(cursor)) throw new Error('Collection cursor did not advance');
      cursors.add(cursor);
    }
    if (windowChanged()) return;
    const anchor = list.returnFocus?.id ?? list.items[list.focusIndex]?.id;
    if (cursor && anchor && !known.has(anchor)) {
      // New rows (or a new random ordering) can shift the card beyond this
      // bounded window. Absence here is not removal: retain the known window.
      dispatch({ type: 'photos.refreshDeferred', generation: list.generation });
      return;
    }
    dispatch({ type: 'photos.pageLoaded', collection: name, generation: list.generation,
      items, nextCursor: cursor, meta, append: false });
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
    if (target.name === 'videos' || target.name === 'video') {
      if (!ports.videos) throw new Error('Video refresh unavailable');
      await ports.videos.load(target); return;
    }
    if (target.name === 'briefing') { await overview(); return; }
    if (target.name === 'house' || target.name === 'calendar') { await house(); return; }
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
        // the return page can reconcile its loaded window.
        if (target.collection) jobs.push(collection());
        break;
      default:
        break;
    }
    await Promise.all(jobs);
  };

  return { overview, house, home, collection, slideshow, route };
}

/** Fire-and-forget wrappers for the throttled `data.changed` path. */
export function toRefetchHandlers(refetchers: Refetchers): {
  house: () => void;
  overview: () => void;
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
    house: swallow(refetchers.house),
    overview: swallow(refetchers.overview),
    home: swallow(refetchers.home),
    collection: swallow(refetchers.collection),
    slideshow: swallow(refetchers.slideshow),
  };
}
