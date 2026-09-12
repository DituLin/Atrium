import { useEffect } from 'react';
import type { HouseLoader } from './house';

/** Only the visible House owns a polling timer; manual recovery may load independently. */
export function useHouseLifecycle(house: HouseLoader, active: boolean, connection: string): void {
  useEffect(() => {
    if (!active) return;
    let timer: ReturnType<typeof setInterval> | undefined;
    const refresh = () => { void house.load().catch(() => {}); };
    const visibility = () => {
      clearInterval(timer); timer = undefined;
      if (document.visibilityState === 'hidden') return;
      refresh(); timer = setInterval(refresh, 30_000);
    };
    visibility();
    document.addEventListener('visibilitychange', visibility);
    return () => { clearInterval(timer); document.removeEventListener('visibilitychange', visibility); };
  }, [house, active, connection]);
}
