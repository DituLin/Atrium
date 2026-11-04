import { MEDIA_MAX_RETRIES, MEDIA_RETRY_MS, classifyMediaStatus } from './media';
import type { MediaOutcome } from './media';

export type PreparationOutcome = MediaOutcome | 'cancelled';
/** A stalled NAS/browser request must not occupy a preparation slot forever. */
export const MEDIA_PROBE_TIMEOUT_MS = 10_000;

interface Task {
  id: string;
  promise: Promise<PreparationOutcome>;
  resolve: (outcome: PreparationOutcome) => void;
  attempts: number;
  done: boolean;
  outcome?: PreparationOutcome;
  queued: boolean;
  timer: number;
  stopProbe?: () => void;
}

/** One bounded, deduplicated preparation window. It never reads full images. */
export class MediaPreparation {
  private tasks = new Map<string, Task>();
  private queue: Task[] = [];
  private active = 0;
  private cancelled = false;

  constructor(private probe: (id: string, signal?: AbortSignal) => Promise<number>, private concurrency = 3) {}

  prepare(id: string): Promise<PreparationOutcome> {
    if (this.cancelled) return Promise.resolve('cancelled');
    const existing = this.tasks.get(id);
    if (existing) return existing.promise;
    let resolve!: Task['resolve'];
    const promise = new Promise<PreparationOutcome>(done => { resolve = done; });
    const task: Task = { id, promise, resolve, attempts: 0, done: false, queued: true, timer: 0 };
    this.tasks.set(id, task);
    this.queue.push(task);
    this.pump();
    return promise;
  }

  /** Stop work outside the visible/current look-ahead window. */
  retain(ids: readonly string[]): void {
    const retained = new Set(ids);
    for (const task of this.tasks.values()) {
      if (!retained.has(task.id) && !task.done) this.finish(task, 'cancelled');
    }
  }

  /** A visible thumbnail can leave and later re-enter the viewport. */
  release(id: string): void {
    const task = this.tasks.get(id);
    if (task) {
      if (!task.done) this.finish(task, 'cancelled');
      // A ready file may be evicted while its card is off-screen. Exhausted
      // failures stay deduplicated, so scrolling cannot restart retry storms.
      if (task.outcome === 'ready' || task.outcome === 'cancelled') this.tasks.delete(id);
    }
  }

  /** A new metadata status is an explicit fresh readiness signal. */
  invalidate(id: string): void {
    const task = this.tasks.get(id);
    if (task) { this.finish(task, 'cancelled'); this.tasks.delete(id); }
  }

  cancel(): void {
    this.cancelled = true;
    for (const task of this.tasks.values()) this.finish(task, 'cancelled');
    this.queue = [];
  }

  private finish(task: Task, outcome: PreparationOutcome): void {
    if (task.done) return;
    task.done = true;
    task.outcome = outcome;
    if (task.timer) window.clearTimeout(task.timer);
    task.stopProbe?.();
    task.resolve(outcome);
  }

  /** Settle once even if an old WebView/transport never honors abort. */
  private probeOnce(task: Task): Promise<number | null> {
    return new Promise(resolve => {
      const controller = typeof AbortController === 'function' ? new AbortController() : null;
      let settled = false;
      let deadline = 0;
      const settle = (status: number | null, abort = false) => {
        if (settled) return;
        settled = true;
        window.clearTimeout(deadline);
        task.stopProbe = undefined;
        if (abort) controller?.abort();
        resolve(status);
      };
      task.stopProbe = () => settle(null, true);
      deadline = window.setTimeout(() => settle(null, true), MEDIA_PROBE_TIMEOUT_MS);
      try {
        void this.probe(task.id, controller?.signal).then(status => settle(status), () => settle(null));
      } catch { settle(null); }
    });
  }

  private pump(): void {
    while (!this.cancelled && this.active < this.concurrency && this.queue.length > 0) {
      const task = this.queue.shift()!;
      task.queued = false;
      if (task.done) continue;
      this.active += 1;
      task.attempts += 1;
      void this.probeOnce(task).then(status => {
        if (task.done || this.cancelled) return;
        const outcome = status === null ? 'error' : classifyMediaStatus(status);
        if (outcome === 'processing' && task.attempts <= MEDIA_MAX_RETRIES) {
          task.timer = window.setTimeout(() => {
            task.timer = 0;
            if (task.done || this.cancelled || task.queued) return;
            task.queued = true;
            this.queue.push(task);
            this.pump();
          }, MEDIA_RETRY_MS);
        } else this.finish(task, outcome);
      }).catch(() => this.finish(task, 'error')).finally(() => {
        this.active -= 1;
        this.pump();
      });
    }
  }
}
