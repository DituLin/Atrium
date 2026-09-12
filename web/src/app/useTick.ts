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
export function useRemoteKeys(onKey: (key: RemoteKey) => boolean | void): void {
  useEffect(() => {
    const handler = (event: KeyboardEvent): void => {
      if (event.defaultPrevented) return;
      const key = mapRemoteKey({ key: event.key, keyCode: event.keyCode });
      if (!key) return;
      if (event.repeat && (key === 'enter' || key === 'back')) { event.preventDefault(); return; }
      // Only a listener that actually handles the key may consume it. A viewer
      // direction listener must leave Back available to the provider.
      if (onKey(key) === true) event.preventDefault();
    };
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, [onKey]);
}
