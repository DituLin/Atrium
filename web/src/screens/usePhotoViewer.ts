import { useEffect, useRef } from 'react';

import { useApp } from '../app/context';
import { neighborId } from '../app/photoViewer';
import type { PhotoViewerState } from '../app/photoViewer';
import { ApiError } from '../core/api';
import { MEDIA_MAX_RETRIES, MEDIA_RETRY_MS, classifyMediaStatus } from '../core/media';
import type { PhotoCollection } from '../types/api';

export function usePhotoViewer(
  photoId: string,
  collection: PhotoCollection | null,
): PhotoViewerState {
  const { state, dispatch, api } = useApp();
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
    let cancelled = false;
    void api
      .getPhoto(photoId)
      .then((detail) => {
        if (cancelled) return;
        dispatch({
          type: 'viewer.loaded',
          generation,
          item: detail.item,
          neighbors: detail.neighbors ?? null,
        });
      })
      .catch((error: unknown) => {
        if (cancelled) return;
        const gone = error instanceof ApiError && error.isGone;
        dispatch({ type: 'viewer.loadFailed', generation, gone });
        if (gone) dispatch({ type: 'photos.itemGone', id: photoId });
      });
    return () => { cancelled = true; requestedRef.current = ''; };
  }, [api, dispatch, photoId, collection, viewer.photoId, viewer.item, status, generation]);

  // Media rules: spinner on 202 with three retries, drop on 404/410, skip on 503.
  useEffect(() => {
    if (viewer.item === null || viewer.photoId !== photoId) return;
    let cancelled = false;
    let timer = 0;
    let processing = false;
    if (viewer.imageFailures > MEDIA_MAX_RETRIES) return;
    const attempt = (round: number): void => {
      void api
        .probeMedia(photoId)
        .then((httpStatus) => {
          if (cancelled) return;
          switch (classifyMediaStatus(httpStatus)) {
            case 'ready':
              // A corrupt image with ready bytes should not retry forever. A
              // processing response, however, needs an actual new img request.
              dispatch({ type: viewer.imageFailures > 0 && !processing ? 'viewer.mediaUnavailable' : 'viewer.mediaReady', id: photoId, generation });
              return; // only the mounted <img> can report the render signal
            case 'processing':
              processing = true;
              dispatch({ type: 'viewer.mediaProcessing', id: photoId, generation });
              if (round <= MEDIA_MAX_RETRIES) {
                timer = window.setTimeout(() => attempt(round + 1), MEDIA_RETRY_MS);
              }
              return;
            case 'gone':
              dispatch({ type: 'viewer.mediaGone', id: photoId, generation });
              dispatch({ type: 'photos.itemGone', id: photoId });
              return;
            default:
              dispatch({ type: 'viewer.mediaUnavailable', id: photoId, generation });
              return;
          }
        })
        .catch(() => {
          if (!cancelled) dispatch({ type: 'viewer.mediaUnavailable', id: photoId, generation });
        });
    };
    attempt(1);
    return () => {
      cancelled = true;
      if (timer) window.clearTimeout(timer);
    };
  }, [api, dispatch, photoId, generation, viewer.item, viewer.photoId, viewer.imageFailures]);

  // At most the two adjacent images are prefetched. Cancel handlers and release
  // references on navigation; prefetch never supplies a command render signal.
  const previous = neighborId(viewer, 'previous');
  const next = neighborId(viewer, 'next');
  useEffect(() => {
    const images = [previous, next].filter((id): id is string => id !== null).map(id => {
      const image = new Image();
      image.decoding = 'async';
      image.src = api.mediaUrl(id, 'preview');
      return image;
    });
    return () => { for (const image of images) image.removeAttribute('src'); };
  }, [api, previous, next]);

  // Failure stays visible until an explicit direction or Back. Failed IDs are
  // skipped for this visit, so even an all-failed collection cannot loop.
  return viewer;
}
