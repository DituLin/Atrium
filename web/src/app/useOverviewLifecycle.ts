import { useEffect } from 'react';
import type { OverviewLoader } from './overview';

/** Only the visible Overview owns a polling timer; manual recovery may load independently. */
export function useOverviewLifecycle(overview: OverviewLoader, active: boolean, connection: string): void {
  useEffect(() => {
    if (!active) return;
    let timer: ReturnType<typeof setInterval> | undefined;
    const refresh = () => { void overview.load().catch(() => {}); };
    const visibility = () => {
      clearInterval(timer); timer = undefined;
      if (document.visibilityState === 'hidden') return;
      refresh(); timer = setInterval(refresh, 30_000);
    };
    visibility();
    document.addEventListener('visibilitychange', visibility);
    return () => { clearInterval(timer); document.removeEventListener('visibilitychange', visibility); };
  }, [overview, active, connection]);
}
