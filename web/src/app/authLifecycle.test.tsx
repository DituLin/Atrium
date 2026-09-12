import { act, render, waitFor } from '@testing-library/react';
import { useEffect } from 'react';
import { expect, it, vi } from 'vitest';
import { AppProvider } from './AppProvider';
import type { AppContextValue } from './context';
import { useApp } from './context';
import { loadLastAuthOkAt, saveLastAuthOkAt } from '../core/authExpiry';

it.each(['app.needsPairing', 'app.authExpired'] as const)('synchronously invalidates in-flight API work on %s', async type => {
  saveLastAuthOkAt(Date.now());
  let app!: AppContextValue;
  let finish!: (response: Response) => void;
  vi.stubGlobal('fetch', vi.fn((path: string) => path.includes('/screens/me')
    ? new Promise<Response>(resolve => { finish = resolve; }) : new Promise(() => {})));
  function Harness() { const current = useApp(); useEffect(() => { app = current; }, [current]); return null; }
  render(<AppProvider><Harness /></AppProvider>);
  await waitFor(() => expect(app).toBeDefined());
  const old = app.api.getScreenSelf();
  const result = old.catch((error: unknown) => error);
  act(() => { app.dispatch({ type }); finish(new Response(JSON.stringify({ id: 'old', name: 'Old', status: 'active' }))); });
  expect(await result).toMatchObject({ name: 'AbortError' });
  expect(loadLastAuthOkAt()).toBeNull();
  expect(type === 'app.needsPairing' ? app.state.needsPairing : app.state.authExpired).toBe(true);
});
