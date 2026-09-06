import { describe, expect, it } from 'vitest';

import type { ThrottleClock } from '../core/throttle';
import { createTopicRefetcher, refetchTargets } from './dataChanged';

const DASHBOARD = { activeCollection: null, slideshowActive: true };
const BROWSING = { activeCollection: 'recent', slideshowActive: true };
const IDLE = { activeCollection: null, slideshowActive: false };

/** Controllable clock: `advance` runs the timers that have come due. */
function fakeClock(): ThrottleClock & { advance: (ms: number) => void } {
  let now = 0;
  let nextHandle = 1;
  const timers = new Map<number, { at: number; fn: () => void }>();
  return {
    now: () => now,
    setTimeout: (fn, ms) => {
      const handle = nextHandle;
      nextHandle += 1;
      timers.set(handle, { at: now + ms, fn });
      return handle;
    },
    clearTimeout: (handle) => void timers.delete(handle),
    advance: (ms) => {
      now += ms;
      for (const [handle, timer] of [...timers]) {
        if (timer.at <= now) {
          timers.delete(handle);
          timer.fn();
        }
      }
    },
  };
}

describe('data.changed: topic mapping', () => {
  it('maps home and nas to the /home snapshot', () => {
    expect(refetchTargets(['home'], DASHBOARD)).toEqual(['home']);
    expect(refetchTargets(['nas'], DASHBOARD)).toEqual(['home']);
  });

  it('refreshes the slideshow list on a photos change from the dashboard', () => {
    expect(refetchTargets(['photos'], DASHBOARD)).toEqual(['home', 'slideshow']);
  });

  it('adds the active collection while the browser is open', () => {
    expect(refetchTargets(['photos'], BROWSING)).toEqual(['home', 'collection', 'slideshow']);
  });

  it('ignores topics the screen does not render and never duplicates a target', () => {
    expect(refetchTargets(['screen'], BROWSING)).toEqual([]);
    expect(refetchTargets(['home', 'nas', 'photos'], IDLE)).toEqual(['home']);
  });
});

describe('data.changed: throttling', () => {
  it('collapses a burst into one refetch per target per second', () => {
    const clock = fakeClock();
    const calls: string[] = [];
    const refetcher = createTopicRefetcher(
      {
        home: () => calls.push('home'),
        collection: () => calls.push('collection'),
        slideshow: () => calls.push('slideshow'),
      },
      { throttleMs: 1000, clock },
    );

    for (let i = 0; i < 20; i += 1) refetcher.notify(['photos'], BROWSING);
    expect(calls).toEqual(['home', 'collection', 'slideshow']);

    // Trailing edge: the burst is answered once more when the window closes.
    clock.advance(1000);
    expect(calls).toEqual([
      'home',
      'collection',
      'slideshow',
      'home',
      'collection',
      'slideshow',
    ]);

    clock.advance(5000);
    expect(calls).toHaveLength(6);
  });

  it('throttles each target independently', () => {
    const clock = fakeClock();
    const calls: string[] = [];
    const refetcher = createTopicRefetcher(
      {
        home: () => calls.push('home'),
        collection: () => calls.push('collection'),
        slideshow: () => calls.push('slideshow'),
      },
      { throttleMs: 1000, clock },
    );

    refetcher.notify(['home'], DASHBOARD);
    expect(calls).toEqual(['home']);
    clock.advance(200);
    // `home` is inside its window, the collection has never run and fires now.
    refetcher.notify(['photos'], BROWSING);
    expect(calls).toEqual(['home', 'collection', 'slideshow']);
    refetcher.cancel();
    clock.advance(2000);
    expect(calls).toEqual(['home', 'collection', 'slideshow']);
  });
});
