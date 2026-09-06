/**
 * Slideshow driver (W-201). All decisions live in `app/slideshow.ts`; this hook
 * only performs effects: fetching the seeded round, running the interval,
 * classifying media responses, and keeping two decoded images.
 *
 * It is mounted by `PhotoPane`, which only exists on the dashboard, so the
 * slideshow cannot run anywhere else (design §7.2, PRD 5.3).
 */

import { useCallback, useEffect, useRef, useState } from 'react';

import { useApp } from '../app/context';
import {
  SLIDESHOW_PAGE_SIZE,
  currentSlide,
  isFixedDisplay,
  nextSlide,
  playableCount,
} from '../app/slideshow';
import { newRandomSeed } from '../core/ids';
import { MEDIA_MAX_RETRIES, MEDIA_RETRY_MS, classifyMediaStatus } from '../core/media';
import type { SlideshowStatus } from '../app/slideshow';
import type { PhotoItem } from '../types/api';
import { decodeImage } from '../ui/decodeImage';
import type { DecodedImage } from '../ui/decodeImage';

/** Fetch the next page while this many photos are still queued. */
export const SLIDESHOW_PREFETCH_AHEAD = 8;

/** Must match the crossfade in `styles.css`; the old layer is dropped after. */
export const CROSSFADE_MS = 800;

export interface SlideshowView {
  status: SlideshowStatus;
  /** The image on screen, once the browser has decoded it. */
  shown: DecodedImage | null;
  /** The previous image, kept one transition long for the crossfade. */
  previous: DecodedImage | null;
  item: PhotoItem | null;
  fixed: boolean;
  count: number;
}

export function useSlideshow(): SlideshowView {
  const { state, dispatch, api } = useApp();
  const slideshow = state.slideshow;
  const current = currentSlide(slideshow);
  const next = nextSlide(slideshow);
  const currentId = current ? current.id : null;
  const nextId = next ? next.id : null;
  const { generation, seed, cursor, roundComplete, paused, intervalMs, index } = slideshow;
  const fixed = isFixedDisplay(slideshow);

  const [shown, setShown] = useState<DecodedImage | null>(null);
  const [previous, setPrevious] = useState<DecodedImage | null>(null);
  const shownRef = useRef<DecodedImage | null>(null);
  const preloadRef = useRef<DecodedImage | null>(null);
  const requestedRef = useRef<string>('');

  const fadeTimerRef = useRef(0);
  const show = useCallback((image: DecodedImage) => {
    setPrevious(shownRef.current);
    shownRef.current = image;
    setShown(image);
    // Once the fade is over the outgoing bitmap is released, so the steady
    // state is exactly two decoded images: the visible one and the preload.
    if (fadeTimerRef.current) window.clearTimeout(fadeTimerRef.current);
    fadeTimerRef.current = window.setTimeout(() => setPrevious(null), CROSSFADE_MS);
  }, []);

  useEffect(
    () => () => {
      if (fadeTimerRef.current) window.clearTimeout(fadeTimerRef.current);
    },
    [],
  );

  // A round with no cursor left is over: a new seed starts the next one.
  useEffect(() => {
    if (seed === null || roundComplete) {
      dispatch({ type: 'slideshow.roundStarted', seed: newRandomSeed() });
    }
  }, [seed, roundComplete, dispatch]);

  // Pages of the current round: the first one, then more as the queue drains.
  useEffect(() => {
    if (seed === null) return;
    const queued = playableCount(slideshow) - index - 1;
    const wantsFirst = slideshow.round.length === 0 && cursor === null;
    const wantsMore = cursor !== null && queued <= SLIDESHOW_PREFETCH_AHEAD;
    if (!wantsFirst && !wantsMore) return;
    const key = `${generation}:${cursor ?? ''}`;
    if (requestedRef.current === key) return;
    requestedRef.current = key;
    void api
      .listPhotos({ collection: 'random', seed, cursor, limit: SLIDESHOW_PAGE_SIZE })
      .then((page) =>
        dispatch({
          type: 'slideshow.pageLoaded',
          seed,
          items: page.items,
          nextCursor: page.next_cursor,
        }),
      )
      .catch(() => {
        // Allow a retry when something else moves the state on; a failed page
        // must not lock the round out for good.
        requestedRef.current = '';
        dispatch({ type: 'slideshow.loadFailed' });
      });
  }, [api, dispatch, generation, seed, cursor, index, slideshow]);

  // The interval itself. It is deliberately not restarted on every advance, so
  // the cadence stays even, and it does not exist at all for a fixed display.
  useEffect(() => {
    if (paused || fixed) return;
    const handle = window.setInterval(() => dispatch({ type: 'slideshow.advance' }), intervalMs);
    return () => window.clearInterval(handle);
  }, [paused, fixed, intervalMs, dispatch]);

  // Media rules for the photo about to be shown (design §7.3).
  useEffect(() => {
    if (currentId === null) return;
    const id = currentId;
    const src = api.mediaUrl(id, 'preview');
    let cancelled = false;
    let timer = 0;

    const skip = (): void => {
      dispatch({ type: 'slideshow.mediaUnavailable', id });
      dispatch({ type: 'slideshow.advance' });
    };

    const attempt = (round: number): void => {
      void api
        .probeMedia(id)
        .then(async (status) => {
          if (cancelled) return;
          switch (classifyMediaStatus(status)) {
            case 'ready': {
              const preloaded = preloadRef.current;
              const decoded =
                preloaded && preloaded.id === id
                  ? { ok: true, image: preloaded }
                  : await decodeImage(id, src);
              if (cancelled) return;
              if (!decoded.ok || !decoded.image) return skip();
              if (preloadRef.current && preloadRef.current.id === id) preloadRef.current = null;
              show(decoded.image);
              dispatch({ type: 'slideshow.displayed', id });
              return;
            }
            case 'processing':
              dispatch({ type: 'slideshow.mediaProcessing', id });
              if (round <= MEDIA_MAX_RETRIES) {
                timer = window.setTimeout(() => attempt(round + 1), MEDIA_RETRY_MS);
              } else {
                dispatch({ type: 'slideshow.advance' });
              }
              return;
            case 'gone':
              dispatch({ type: 'slideshow.mediaGone', id });
              return;
            default:
              return skip();
          }
        })
        .catch(() => {
          if (!cancelled) skip();
        });
    };

    attempt(1);
    return () => {
      cancelled = true;
      if (timer) window.clearTimeout(timer);
    };
  }, [currentId, generation, api, dispatch, show]);

  // Preload exactly one image ahead; the reference is replaced, never grown.
  useEffect(() => {
    if (nextId === null) return;
    if (preloadRef.current && preloadRef.current.id === nextId) return;
    let cancelled = false;
    void decodeImage(nextId, api.mediaUrl(nextId, 'preview')).then((result) => {
      if (!cancelled && result.ok) preloadRef.current = result.image;
    });
    return () => {
      cancelled = true;
    };
  }, [nextId, api]);

  return {
    status: slideshow.status,
    shown,
    previous,
    item: current,
    fixed,
    count: playableCount(slideshow),
  };
}
