/**
 * Photo viewer driver (W-203). Loads the item with its neighbours, applies the
 * media rules of design §7.3, pauses the slideshow while the viewer is open,
 * and leaves the route when the photo turns out to be unusable.
 */

import { useEffect, useRef } from 'react';

import { useApp } from '../app/context';
import { shouldLeave, skipTarget } from '../app/photoViewer';
import type { PhotoViewerState } from '../app/photoViewer';
import { ApiError } from '../core/api';
import { MEDIA_MAX_RETRIES, MEDIA_RETRY_MS, classifyMediaStatus } from '../core/media';
import type { PhotoCollection } from '../types/api';

export function usePhotoViewer(
  photoId: string,
  collection: PhotoCollection | null,
): PhotoViewerState {
  const { state, dispatch, api, goBack } = useApp();
  const viewer = state.viewer;
  const requestedRef = useRef('');
  const { status, generation } = viewer;

  // The slideshow is paused for as long as a single photo is on screen
  // (PRD 5.3); leaving the route resumes it.
  useEffect(() => {
    dispatch({ type: 'slideshow.pause' });
    return () => dispatch({ type: 'slideshow.resume' });
  }, [dispatch]);

  useEffect(() => {
    dispatch({ type: 'viewer.open', photoId, collection });
  }, [photoId, collection, dispatch]);

  useEffect(() => {
    if (viewer.photoId !== photoId || viewer.item !== null) return;
    if (status !== 'loading') return;
    const key = `${generation}:${photoId}`;
    if (requestedRef.current === key) return;
    requestedRef.current = key;
    void api
      .getPhoto(photoId, collection ?? undefined)
      .then((detail) =>
        dispatch({
          type: 'viewer.loaded',
          generation,
          item: detail.item,
          neighbors: detail.neighbors ?? null,
        }),
      )
      .catch((error: unknown) => {
        const gone = error instanceof ApiError && error.isGone;
        dispatch({ type: 'viewer.loadFailed', generation, gone });
      });
  }, [api, dispatch, photoId, collection, viewer.photoId, viewer.item, status, generation]);

  // Media rules: spinner on 202 with three retries, drop on 404/410, skip on 503.
  useEffect(() => {
    if (viewer.item === null || viewer.photoId !== photoId) return;
    let cancelled = false;
    let timer = 0;
    const attempt = (round: number): void => {
      void api
        .probeMedia(photoId)
        .then((httpStatus) => {
          if (cancelled) return;
          switch (classifyMediaStatus(httpStatus)) {
            case 'ready':
              return; // the <img> element takes over and reports onload
            case 'processing':
              dispatch({ type: 'viewer.mediaProcessing', id: photoId });
              if (round <= MEDIA_MAX_RETRIES) {
                timer = window.setTimeout(() => attempt(round + 1), MEDIA_RETRY_MS);
              }
              return;
            case 'gone':
              dispatch({ type: 'viewer.mediaGone', id: photoId });
              return;
            default:
              dispatch({ type: 'viewer.mediaUnavailable', id: photoId });
              return;
          }
        })
        .catch(() => {
          if (!cancelled) dispatch({ type: 'viewer.mediaUnavailable', id: photoId });
        });
    };
    attempt(1);
    return () => {
      cancelled = true;
      if (timer) window.clearTimeout(timer);
    };
  }, [api, dispatch, photoId, viewer.item, viewer.photoId]);

  // An unusable photo skips to a neighbour, or gives up and goes back.
  useEffect(() => {
    if (viewer.status !== 'missing') return;
    if (shouldLeave(viewer)) {
      goBack();
      return;
    }
    const target = skipTarget(viewer);
    if (target) {
      dispatch({
        type: 'router.navigate',
        route: collection
          ? { name: 'photo', photoId: target, collection }
          : { name: 'photo', photoId: target },
      });
    }
  }, [viewer, collection, dispatch, goBack]);

  return viewer;
}
