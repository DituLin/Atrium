/**
 * Collection browser driver (W-202): opens the collection, loads the first
 * page, and pulls the next one while the focus approaches the end of the grid.
 * Every decision (what "near the end" means, which page is stale) lives in
 * `app/photoList.ts`.
 */

import { useEffect, useRef } from 'react';

import { useApp } from '../app/context';
import { COLLECTION_PAGE_SIZE, shouldLoadMore } from '../app/photoList';
import type { PhotoListState } from '../app/photoList';
import type { PhotoCollection } from '../types/api';

export function usePhotoList(collection: PhotoCollection): PhotoListState {
  const { state, dispatch, api } = useApp();
  const list = state.collection;
  const requestedRef = useRef('');

  useEffect(() => {
    dispatch({ type: 'photos.open', collection });
  }, [collection, dispatch]);

  useEffect(() => {
    if (list.collection !== collection) return;
    const first = list.status === 'loading' && list.items.length === 0;
    const more = shouldLoadMore(list);
    if (!first && !more) return;
    const key = `${list.generation}:${list.cursor ?? ''}`;
    if (requestedRef.current === key) return;
    requestedRef.current = key;
    const cursor = first ? null : list.cursor;
    if (!first) dispatch({ type: 'photos.pageRequested' });
    void api
      .listPhotos({ collection, cursor, limit: COLLECTION_PAGE_SIZE })
      .then((page) =>
        dispatch({
          type: 'photos.pageLoaded',
          collection,
          generation: list.generation,
          items: page.items,
          nextCursor: page.next_cursor,
          meta: page.meta ?? null,
          append: !first,
        }),
      )
      .catch(() => {
        requestedRef.current = '';
        dispatch({ type: 'photos.loadFailed', generation: list.generation });
      });
  }, [api, dispatch, collection, list]);

  return list;
}
