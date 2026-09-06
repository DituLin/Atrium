/**
 * Collection browser (W-202, design §7.1). Left/Right/Up/Down move the focus,
 * Enter opens the photo viewer, Back returns to the dashboard (handled once in
 * `AppProvider`). Every empty state says why it is empty and offers a way on.
 */

import type { ReactElement } from 'react';
import { useCallback } from 'react';

import { useApp } from '../app/context';
import { clockOptions } from '../app/homeSelect';
import { GRID_COLUMNS, emptyKind, focusedItem } from '../app/photoList';
import type { PhotoCollection } from '../types/api';
import type { RemoteKey } from '../ui/keys';
import { PhotoGrid } from './PhotoGrid';
import { usePhotoList } from './usePhotoList';

const TITLES: Readonly<Record<PhotoCollection, string>> = {
  recent: 'Recently added',
  captured_today: 'Taken today',
  random: 'Family photos',
  all: 'All photos',
};

const EMPTY_TEXT: Readonly<Record<string, string>> = {
  recent_baseline_only:
    'Nothing new since the first library import. The import itself is not counted as new.',
  captured_today: 'No photo has been taken today yet.',
  generic: 'This collection has no photos to show.',
};

export function PhotosScreen(): ReactElement {
  const { state, dispatch } = useApp();
  const route = state.router.route;
  const collection: PhotoCollection = route.name === 'photos' ? route.collection : 'recent';
  const list = usePhotoList(collection);
  const options = clockOptions(state.home);

  const openDashboard = useCallback(
    () => dispatch({ type: 'router.navigate', route: { name: 'dashboard' } }),
    [dispatch],
  );

  const onDirection = useCallback(
    (direction: RemoteKey): boolean => {
      dispatch({ type: 'photos.move', direction });
      return true;
    },
    [dispatch],
  );

  const onActivate = useCallback(
    (index: number) => {
      const item = list.items[index] ?? focusedItem(list);
      if (!item) return;
      dispatch({
        type: 'router.navigate',
        route: { name: 'photo', photoId: item.id, collection },
      });
    },
    [dispatch, list, collection],
  );

  const onFocusIndex = useCallback(
    (index: number) => dispatch({ type: 'photos.focus', index }),
    [dispatch],
  );

  const empty = emptyKind(list);
  const unknownCount = list.meta?.unknown_captured_count ?? 0;

  return (
    <div className="screen screen--photos">
      <header className="photos__header">
        <h1 className="photos__title">{TITLES[collection]}</h1>
        <p className="photos__count">
          {list.status === 'loading' && list.items.length === 0
            ? 'Loading…'
            : `${list.items.length}${list.cursor ? '+' : ''} photos`}
        </p>
      </header>

      {collection === 'captured_today' && unknownCount > 0 ? (
        <p className="photos__note" role="status">
          {unknownCount} photo{unknownCount === 1 ? '' : 's'} have no capture time and are not
          listed here.
        </p>
      ) : null}

      {list.status === 'error' ? (
        <p className="photos__note photos__note--bad" role="status">
          <span aria-hidden="true">■</span> The collection could not be loaded.
        </p>
      ) : null}

      {empty !== 'none' ? (
        <div className="photos__emptystate">
          <p className="photos__empty">{EMPTY_TEXT[empty]}</p>
          <button type="button" className="button" onClick={openDashboard} autoFocus>
            Go to slideshow
          </button>
        </div>
      ) : (
        <PhotoGrid
          items={list.items}
          focusIndex={list.focusIndex}
          columns={GRID_COLUMNS}
          options={options}
          onDirection={onDirection}
          onActivate={onActivate}
          onFocusIndex={onFocusIndex}
        />
      )}
    </div>
  );
}
