/**
 * One-second local tick that drives the clock, plus the remote-key listener.
 * Both are window-level concerns kept out of the provider body for clarity.
 */

import { useEffect } from 'react';

import type { RemoteKey } from '../ui/keys';
import { mapRemoteKey } from '../ui/keys';

export function useSecondTick(onTick: (nowMs: number) => void): void {
  useEffect(() => {
    const handle = window.setInterval(() => onTick(Date.now()), 1000);
    return () => window.clearInterval(handle);
  }, [onTick]);
}

/** Global remote handler; screens handle direction keys locally via focus. */
export function useRemoteKeys(onKey: (key: RemoteKey) => void): void {
  useEffect(() => {
    const handler = (event: KeyboardEvent): void => {
      const key = mapRemoteKey({ key: event.key, keyCode: event.keyCode });
      if (!key) return;
      if (key === 'back') {
        // Back must never navigate the browser away from the SPA.
        event.preventDefault();
      }
      onKey(key);
    };
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, [onKey]);
}
