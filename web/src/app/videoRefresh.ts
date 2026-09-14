import type { AppRoute } from './router';
import { routesEqual } from './router';

/** Awaitable bridge from refresh commands to the currently mounted video page. */
export class VideoRefresh {
  private active: { route: AppRoute; run: (signal: AbortSignal) => Promise<void>; abort: AbortController } | null = null;
  register(route: AppRoute, run: (signal: AbortSignal) => Promise<void>): () => void {
    this.active?.abort.abort();
    const entry = { route, run, abort: new AbortController() };
    this.active = entry;
    return () => { entry.abort.abort(); if (this.active === entry) this.active = null; };
  }
  async load(route: AppRoute): Promise<void> {
    const entry = this.active;
    if (!entry || !routesEqual(entry.route, route)) throw new Error('Video refresh unavailable');
    await entry.run(entry.abort.signal);
    if (entry.abort.signal.aborted) throw new DOMException('Video page changed', 'AbortError');
  }
}
