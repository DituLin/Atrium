import { useCallback, useEffect, useMemo, useRef } from 'react';
import type { ApiClient } from '../core/api';
import { MediaPreparation } from '../core/mediaPreparation';

/** Lazily recreated after StrictMode cleanup; old callbacks own their session. */
export function useMediaPreparation(api: ApiClient, generation: number, blocked: boolean, variant: 'preview' | 'thumb' = 'preview') {
  const owner = useMemo(() => ({ api, generation, blocked, variant }), [api, generation, blocked, variant]);
  const ref = useRef<{ owner: typeof owner; session: MediaPreparation } | null>(null);
  const session = useCallback(() => {
    if (ref.current?.owner !== owner) {
      ref.current?.session.cancel();
      ref.current = { owner, session: new MediaPreparation((id, signal) => api.probeMedia(id, variant, signal)) };
    }
    return ref.current.session;
  }, [api, owner, variant]);
  useEffect(() => () => {
    if (ref.current?.owner === owner) { ref.current.session.cancel(); ref.current = null; }
  }, [owner]);
  const prepare = useCallback((id: string) => blocked ? Promise.resolve('cancelled' as const) : session().prepare(id), [blocked, session]);
  const retain = useCallback((ids: readonly string[]) => { if (ref.current?.owner === owner) ref.current.session.retain(ids); }, [owner]);
  const release = useCallback((id: string) => { if (ref.current?.owner === owner) ref.current.session.release(id); }, [owner]);
  const invalidate = useCallback((id: string) => { if (ref.current?.owner === owner) ref.current.session.invalidate(id); }, [owner]);
  return { prepare, retain, release, invalidate };
}
