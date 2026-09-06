/**
 * Vitest setup.
 *
 * Two environment quirks are patched here:
 *  - `__CLIENT_VERSION__` is a Vite `define`, so it does not exist under Vitest.
 *  - Node 25 exposes an experimental global `localStorage` that shadows the
 *    jsdom one and is unusable without `--localstorage-file`. The app must work
 *    with a real Storage, so a minimal in-memory one is installed when the
 *    ambient object is not a working Storage.
 */

import { afterEach, vi } from 'vitest';
import { cleanup } from '@testing-library/react';

(globalThis as unknown as { __CLIENT_VERSION__: string }).__CLIENT_VERSION__ = '0.0.0+test';

function memoryStorage(): Storage {
  const map = new Map<string, string>();
  return {
    get length() {
      return map.size;
    },
    clear: () => map.clear(),
    getItem: (key: string) => map.get(key) ?? null,
    key: (index: number) => [...map.keys()][index] ?? null,
    removeItem: (key: string) => void map.delete(key),
    setItem: (key: string, value: string) => void map.set(key, String(value)),
  };
}

function usable(candidate: unknown): boolean {
  return (
    typeof candidate === 'object' &&
    candidate !== null &&
    typeof (candidate as Storage).setItem === 'function'
  );
}

if (!usable((window as Window & typeof globalThis).localStorage)) {
  Object.defineProperty(window, 'localStorage', {
    value: memoryStorage(),
    configurable: true,
    writable: true,
  });
}

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  try {
    window.localStorage.clear();
  } catch {
    /* ignore */
  }
});
