/**
 * `data.changed` handling (W-205, design §7.3, §9).
 *
 * The server coalesces changes per session (250 ms) and the client throttles
 * again at 1 s, per target rather than per message: a burst that touches both
 * `home` and `photos` must not make the two refetches wait for each other, and
 * a stream of `photos` events must not restart the visible slide.
 */

import { createThrottle, defaultThrottleClock } from '../core/throttle';
import type { Throttled, ThrottleClock } from '../core/throttle';
import type { DataChangedTopic } from '../types/ws';

export const DATA_CHANGED_THROTTLE_MS = 1000;

/** What a change notification can make the client re-read. */
export type RefetchTarget = 'home' | 'collection' | 'slideshow';

export interface RefetchContext {
  /** Collection currently on screen, or null when the browser is not open. */
  activeCollection: string | null;
  /** True while the dashboard slideshow holds a round. */
  slideshowActive: boolean;
}

/**
 * Maps topics to targets. `nas` and `home` both land in the `/home` snapshot
 * (the NAS widget is part of it, design §6.7); `photos` additionally refreshes
 * whatever photo list is on screen.
 */
export function refetchTargets(
  topics: readonly DataChangedTopic[],
  context: RefetchContext,
): readonly RefetchTarget[] {
  const targets = new Set<RefetchTarget>();
  for (const topic of topics) {
    switch (topic) {
      case 'home':
      case 'nas':
        targets.add('home');
        break;
      case 'photos':
        targets.add('home');
        if (context.activeCollection !== null) targets.add('collection');
        if (context.slideshowActive) targets.add('slideshow');
        break;
      default:
        break;
    }
  }
  return [...targets];
}

export type RefetchHandlers = Readonly<Record<RefetchTarget, () => void>>;

export interface TopicRefetcher {
  /** Feed one `data.changed` payload; runs at most one refetch per target/s. */
  notify(topics: readonly DataChangedTopic[], context: RefetchContext): void;
  cancel(): void;
}

export function createTopicRefetcher(
  handlers: RefetchHandlers,
  options: { throttleMs?: number; clock?: ThrottleClock } = {},
): TopicRefetcher {
  const intervalMs = options.throttleMs ?? DATA_CHANGED_THROTTLE_MS;
  const clock = options.clock ?? defaultThrottleClock;
  const throttled: Record<RefetchTarget, Throttled<[]>> = {
    home: createThrottle(intervalMs, handlers.home, clock),
    collection: createThrottle(intervalMs, handlers.collection, clock),
    slideshow: createThrottle(intervalMs, handlers.slideshow, clock),
  };

  return {
    notify(topics, context) {
      for (const target of refetchTargets(topics, context)) throttled[target]();
    },
    cancel() {
      throttled.home.cancel();
      throttled.collection.cancel();
      throttled.slideshow.cancel();
    },
  };
}
