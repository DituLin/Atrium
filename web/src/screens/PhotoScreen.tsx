/**
 * Single-photo viewer (W-203). Left/Right walk the collection through the
 * `neighbors` the server returned, Back returns to the previous route, and the
 * image's `onload` is the render signal a `show` command's ack waits for
 * (design §7.2).
 */

import type { ReactElement } from 'react';
import { useCallback } from 'react';

import { useApp } from '../app/context';
import { clockOptions } from '../app/homeSelect';
import { isWaiting, neighborId } from '../app/photoViewer';
import { useRemoteKeys } from '../app/useTick';
import { photoCaption } from '../ui/photoCaption';
import { usePhotoViewer } from './usePhotoViewer';

export function PhotoScreen(): ReactElement {
  const { state, dispatch, api, goBack } = useApp();
  const route = state.router.route;
  const photoId = route.name === 'photo' ? route.photoId : '';
  const collection = route.name === 'photo' ? (route.collection ?? null) : null;
  const viewer = usePhotoViewer(photoId, collection);

  const go = useCallback(
    (direction: 'previous' | 'next') => {
      const target = neighborId(viewer, direction);
      if (!target) return;
      dispatch({
        type: 'router.navigate',
        route: collection
          ? { name: 'photo', photoId: target, collection }
          : { name: 'photo', photoId: target },
      });
    },
    [viewer, collection, dispatch],
  );

  useRemoteKeys(
    useCallback(
      (key) => {
        if (key === 'left') go('previous');
        else if (key === 'right') go('next');
      },
      [go],
    ),
  );

  const caption = viewer.item ? photoCaption(viewer.item, clockOptions(state.home)) : null;

  return (
    <div className="screen screen--photo">
      <div className="viewer">
        {viewer.item ? (
          <img
            className="viewer__image"
            src={api.mediaUrl(viewer.item.id, 'preview')}
            alt=""
            key={viewer.item.id}
            onLoad={() => dispatch({ type: 'viewer.rendered', id: viewer.item?.id ?? '' })}
          />
        ) : null}
        {isWaiting(viewer) ? (
          <p className="viewer__status" role="status">
            <span className="spinner" aria-hidden="true" /> Preparing this photo…
          </p>
        ) : null}
        {viewer.status === 'error' ? (
          <p className="viewer__status" role="status">
            <span aria-hidden="true">■</span> This photo could not be loaded.
          </p>
        ) : null}
      </div>

      <footer className="viewer__bar">
        <p className="viewer__caption">
          {caption && caption.date !== '' ? caption.date : 'Capture date unknown'}
          {caption?.estimated ? <span className="viewer__estimated"> estimated</span> : null}
        </p>
        <div className="viewer__hints">
          <span className={neighborId(viewer, 'previous') ? '' : 'is-disabled'}>◀ Previous</span>
          <span className={neighborId(viewer, 'next') ? '' : 'is-disabled'}>Next ▶</span>
          <button type="button" className="button" onClick={goBack}>
            Back
          </button>
        </div>
      </footer>
    </div>
  );
}
