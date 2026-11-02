/**
 * Slideshow driver (W-201). All decisions live in `app/slideshow.ts`; this hook
 * only performs effects: fetching the daily round, running the interval,
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
import { clockOptions } from '../app/homeSelect';
import { clearMemoriesProgress, loadMemoriesProgress, saveMemoriesProgress } from '../app/memoriesProgress';
import { homeDayKey, serverNow } from '../core/clock';
import { ApiError } from '../core/api';
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
  const { generation, cursor, roundComplete, paused, intervalMs, index } = slideshow;
  const blocked = state.authExpired || state.needsPairing;
  const observedDay = state.home ? homeDayKey(serverNow(state.clock, state.nowMs), clockOptions(state.home)) : null;
  const fixed = isFixedDisplay(slideshow);

  const [bootNonce, setBootNonce] = useState(0);
  const [shown, setShown] = useState<DecodedImage | null>(null);
  const [previous, setPrevious] = useState<DecodedImage | null>(null);
  const shownRef = useRef<DecodedImage | null>(null);
  const preloadRef = useRef<DecodedImage | null>(null);
  const requestedRef = useRef<string>('');
  const retryAtRef = useRef(0);
  const bootRef = useRef(false);
  const dayRef = useRef<string | null>(null);
  const emptyTailRetryRef = useRef<string | null>(null);
  const lifetimeRef = useRef(0);
  useEffect(() => {
    lifetimeRef.current += 1; requestedRef.current = '';
    return () => { lifetimeRef.current += 1; requestedRef.current = ''; };
  }, []);
  const latestRef = useRef(state);
  useEffect(() => { latestRef.current = state; }, [state]);

  const fadeTimerRef = useRef(0);
  const show = useCallback((image: DecodedImage) => {
    emptyTailRetryRef.current = null;
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

  // Identify the authenticated screen before considering its local opaque bookmark.
  useEffect(() => {
    if (blocked || slideshow.memoriesMode || bootRef.current) return;
    bootRef.current = true;
    let cancelled = false;
    let retry = 0;
    void api.getScreenSelf().then(screen => {
      if (cancelled || latestRef.current.authExpired || latestRef.current.needsPairing) return;
      const progress = loadMemoriesProgress(screen.id);
      dayRef.current = observedDay;
      dispatch({ type: 'slideshow.memoriesStarted', resumeCursor: progress?.resumeCursor ?? null });
    }).catch(() => { if (!cancelled) retry = window.setTimeout(() => { bootRef.current = false; setBootNonce(value => value + 1); }, 30_000); });
    return () => { cancelled = true; bootRef.current = false; if (retry) window.clearTimeout(retry); };
  }, [api, blocked, dispatch, slideshow.memoriesMode, observedDay, bootNonce]);

  // Home time is authoritative. This also runs after returning from another tab
  // or waking the TV, including empty and single-photo libraries.
  useEffect(() => {
    if (blocked || !slideshow.memoriesMode || !observedDay) return;
    if (!slideshow.memories || dayRef.current === observedDay) return;
    dayRef.current = observedDay;
    if (slideshow.memories && slideshow.memories.day !== observedDay) {
      requestedRef.current = ''; retryAtRef.current = 0;
      dispatch({ type: 'slideshow.memoriesStarted', resumeCursor: null });
    }
  }, [observedDay, blocked, dispatch, slideshow.memoriesMode, slideshow.memories]);

  useEffect(() => {
    if (blocked) {
      clearMemoriesProgress(); shownRef.current = null; preloadRef.current = null;
      return;
    }
    const meta = slideshow.memories;
    if (meta && slideshow.displayedId && slideshow.displayedCursor && slideshow.round.some(item => item.id === slideshow.displayedId)) {
      saveMemoriesProgress({ v: 1, day: meta.day, screenID: meta.screen_id, resumeCursor: slideshow.displayedCursor });
    }
  }, [blocked, slideshow.memories, slideshow.displayedId, slideshow.displayedCursor, slideshow.round]);

  // A completed daily cycle restarts with no cursor, yielding the SAME daily
  // hash order. Failed libraries have a bounded one-minute retry, never a spin.
  useEffect(() => {
    if (blocked || !slideshow.memoriesMode) return;
    const emptyTail = slideshow.loaded && slideshow.round.length === 0 && cursor === null && (slideshow.memories?.total ?? 0) > 0;
    const emptyKey = slideshow.memories ? `${slideshow.memories.screen_id}:${slideshow.memories.day}` : null;
    if (emptyTail && emptyTailRetryRef.current !== emptyKey) {
      // The saved/current anchor was at the deleted tail. Earlier authorized
      // photos still exist, so wrap once immediately, then back off if needed.
      emptyTailRetryRef.current = emptyKey;
      requestedRef.current = ''; retryAtRef.current = 0;
      dispatch({ type: 'slideshow.memoriesStarted', resumeCursor: null }); return;
    }
    const failed = emptyTail || slideshow.loaded && slideshow.round.length > 0 && playableCount(slideshow) === 0 && cursor === null;
    if (failed && retryAtRef.current === 0) retryAtRef.current = Date.now() + 60_000;
    if (roundComplete || (failed && Date.now() >= retryAtRef.current)) {
      requestedRef.current = ''; retryAtRef.current = 0;
      dispatch({ type: 'slideshow.memoriesStarted', resumeCursor: null });
    }
  }, [blocked, cursor, dispatch, roundComplete, slideshow, state.nowMs]);

  // Fetch the resumed inclusive first window, then exclusive continuation pages.
  useEffect(() => {
    if (blocked || !slideshow.memoriesMode) return;
    const queued = playableCount(slideshow) - index - 1;
    const wantsFirst = !slideshow.loaded;
    const wantsMore = cursor !== null && queued <= SLIDESHOW_PREFETCH_AHEAD;
    if (!wantsFirst && !wantsMore) return;
    const requestCursor = wantsFirst ? slideshow.initialCursor : cursor;
    const key = `${generation}:${requestCursor ?? ''}`;
    if (requestedRef.current === key || Date.now() < retryAtRef.current) return;
    requestedRef.current = key;
    const lifetime = lifetimeRef.current;
    void api.listMemories({ cursor: requestCursor, limit: SLIDESHOW_PAGE_SIZE }).then(page => {
      if (lifetimeRef.current !== lifetime || latestRef.current.authExpired || latestRef.current.needsPairing || latestRef.current.slideshow.generation !== generation) return;
      retryAtRef.current = 0;
      dispatch({ type: 'slideshow.memoriesLoaded', generation, page, baseCursor: wantsFirst ? undefined : requestCursor });
    }).catch((error: unknown) => {
      if (lifetimeRef.current !== lifetime || latestRef.current.slideshow.generation !== generation) return;
      if (wantsFirst && requestCursor && error instanceof ApiError && error.status === 400) {
        clearMemoriesProgress(); requestedRef.current = ''; retryAtRef.current = 0;
        dispatch({ type: 'slideshow.memoriesStarted', resumeCursor: null }); return;
      }
      requestedRef.current = ''; retryAtRef.current = Date.now() + 30_000;
      dispatch({ type: 'slideshow.loadFailed' });
    });
  }, [api, blocked, dispatch, generation, cursor, index, slideshow, state.nowMs]);

  // A refresh that confirms removal may not retain revoked source pixels.
  useEffect(() => {
    if (slideshow.loaded && shownRef.current && !slideshow.round.some(item => item.id === shownRef.current?.id)) {
      shownRef.current = null; preloadRef.current = null;
    }
  }, [slideshow.loaded, slideshow.round]);

  // The interval itself. It is deliberately not restarted on every advance, so
  // the cadence stays even, and it does not exist at all for a fixed display.
  useEffect(() => {
    if (paused || fixed) return;
    const handle = window.setInterval(() => dispatch({ type: 'slideshow.advance' }), intervalMs);
    return () => window.clearInterval(handle);
  }, [paused, fixed, intervalMs, dispatch]);

  // Media rules for the photo about to be shown (design §7.3).
  useEffect(() => {
    if (currentId === null || blocked) return;
    const id = currentId;
    if (shownRef.current?.id === id) {
      // A fresh authorized window retained this image: keep the same decoded
      // bitmap while establishing its bookmark under the new request barrier.
      dispatch({ type: 'slideshow.displayed', id, generation }); return;
    }
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
              if (cancelled || latestRef.current.slideshow.generation !== generation || latestRef.current.authExpired || latestRef.current.needsPairing) return;
              if (!decoded.ok || !decoded.image) return skip();
              if (preloadRef.current && preloadRef.current.id === id) preloadRef.current = null;
              show(decoded.image);
              dispatch({ type: 'slideshow.displayed', id, generation });
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
  }, [currentId, generation, api, dispatch, show, blocked]);

  // Preload exactly one image ahead; the reference is replaced, never grown.
  useEffect(() => {
    if (nextId === null || blocked) { preloadRef.current = null; return; }
    if (preloadRef.current && preloadRef.current.id === nextId) return;
    let cancelled = false;
    void decodeImage(nextId, api.mediaUrl(nextId, 'preview')).then((result) => {
      if (!cancelled && result.ok && latestRef.current.slideshow.generation === generation) preloadRef.current = result.image;
    });
    return () => {
      cancelled = true;
    };
  }, [nextId, api, blocked, generation]);

  return {
    status: slideshow.status,
    shown: blocked || (slideshow.loaded && shown && !slideshow.round.some(item => item.id === shown.id)) ? null : shown,
    previous: blocked || (slideshow.loaded && previous && !slideshow.round.some(item => item.id === previous.id)) ? null : previous,
    item: current,
    fixed,
    count: playableCount(slideshow),
  };
}
