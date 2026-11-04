import { StrictMode } from 'react';
import { act, fireEvent, render } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { AppContext } from '../app/context';
import { createInitialState } from '../app/state';
import type { ApiClient } from '../core/api';
import type { PhotoItem, PreviewStatus } from '../types/api';
import { PhotoGrid } from './PhotoGrid';

const observers = new Map<Element, IntersectionObserverCallback>();
class VisibleObserver {
  constructor(private callback: IntersectionObserverCallback) {}
  observe(element: Element) { observers.set(element, this.callback); }
  disconnect() { for (const [element, callback] of observers) if (callback === this.callback) observers.delete(element); }
}
const flush = async () => { await act(async () => { for (let i = 0; i < 12; i++) await Promise.resolve(); }); };
const intersect = (element: Element, isIntersecting: boolean) => act(() => {
  const callback = observers.get(element);
  expect(callback).toBeDefined();
  callback!([{ target: element, isIntersecting } as IntersectionObserverEntry], {} as IntersectionObserver);
});
function setup(strict = false) {
  const probeMedia = vi.fn(async (_id: string, _variant?: string) => 200);
  const api = { probeMedia } as unknown as ApiClient;
  const state = createInitialState(0);
  const items = Array.from({ length: 50 }, (_, index) => ({ id: `p${index}`, captured_at: null, captured_confidence: 'unknown',
    preview: { status: 'evicted' }, urls: { thumb: `/thumb/p${index}` } }) as PhotoItem);
  const tree = () => {
    const grid = <PhotoGrid items={items} sections={[]} focusIndex={0} columns={4} options={{ timezone: 'UTC', utcOffsetSeconds: 0 }}
      onDirection={() => false} onActivate={() => {}} onFocusIndex={() => {}} />;
    return <AppContext.Provider value={{ state, dispatch() {}, api, house: {} as never, overview: {} as never, goBack() {}, clientVersion: 'test' }}>
      {strict ? <StrictMode>{grid}</StrictMode> : grid}
    </AppContext.Provider>;
  };
  const rendered = render(tree());
  return { ...rendered, probeMedia, frames: () => [...rendered.container.querySelectorAll('.tile__frame')],
    updateStatus(index: number, status: PreviewStatus) { items[index] = { ...items[index]!, preview: { ...items[index]!.preview, status } }; rendered.rerender(tree()); },
  };
}
beforeEach(() => { vi.useFakeTimers(); observers.clear(); vi.stubGlobal('IntersectionObserver', VisibleObserver); });
afterEach(() => { vi.useRealTimers(); vi.unstubAllGlobals(); });

it('prepares only visible cold thumbnails and mounts each image only after a successful probe', async () => {
  const h = setup();
  h.probeMedia.mockResolvedValueOnce(202).mockResolvedValue(206);
  await flush();
  expect(h.probeMedia).not.toHaveBeenCalled();
  expect(h.container.querySelectorAll('img')).toHaveLength(0);
  intersect(h.frames()[0]!, true); await flush();
  expect(h.probeMedia).toHaveBeenCalledWith('p0', 'thumb', expect.any(AbortSignal));
  expect(h.container.querySelectorAll('img')).toHaveLength(0);
  await act(async () => { vi.advanceTimersByTime(5000); }); await flush();
  expect(h.container.querySelector('img')?.getAttribute('src')).toBe('/thumb/p0');
  expect(h.probeMedia).toHaveBeenCalledTimes(2);
});

it('stops thumbnail retries when it leaves the viewport or the grid unmounts', async () => {
  const h = setup(); h.probeMedia.mockResolvedValue(202);
  intersect(h.frames()[0]!, true); intersect(h.frames()[1]!, true); await flush();
  intersect(h.frames()[0]!, false); h.unmount();
  await act(async () => { vi.advanceTimersByTime(20_000); }); await flush();
  expect(h.probeMedia).toHaveBeenCalledTimes(2);
});

it('does not restart exhausted thumbnail preparation on repeated visibility notifications', async () => {
  const h = setup(true); h.probeMedia.mockResolvedValue(202);
  intersect(h.frames()[0]!, true); await flush();
  for (let retry = 0; retry < 3; retry++) { await act(async () => { vi.advanceTimersByTime(5000); }); await flush(); }
  intersect(h.frames()[0]!, false); intersect(h.frames()[0]!, true); await flush();
  await act(async () => { vi.advanceTimersByTime(10_000); }); await flush();
  expect(h.probeMedia).toHaveBeenCalledTimes(4);
  expect(h.container.querySelectorAll('img')).toHaveLength(0);
});

it('honors newly ready metadata after a thumbnail exhausted its bounded processing retries', async () => {
  const h = setup(); h.probeMedia.mockResolvedValue(202);
  intersect(h.frames()[0]!, true); await flush();
  for (let retry = 0; retry < 3; retry++) { await act(async () => { vi.advanceTimersByTime(5000); }); await flush(); }
  expect(h.container.querySelectorAll('img')).toHaveLength(0);
  h.probeMedia.mockResolvedValue(206);
  h.updateStatus(0, 'ready'); await flush();
  expect(h.container.querySelector('img')?.getAttribute('src')).toBe('/thumb/p0');
  expect(h.probeMedia).toHaveBeenCalledTimes(5);
});

it('recovers a failed full thumbnail fetch after an explicit ready metadata refresh', async () => {
  const h = setup(); h.probeMedia.mockResolvedValue(206);
  intersect(h.frames()[0]!, true); await flush();
  fireEvent.error(h.container.querySelector('img')!);
  expect(h.container.querySelectorAll('img')).toHaveLength(0);
  h.updateStatus(0, 'ready'); await flush();
  expect(h.container.querySelector('img')?.getAttribute('src')).toBe('/thumb/p0');
});

it('rechecks a previously ready thumbnail on viewport reentry when only its cache file was evicted', async () => {
  const h = setup(); h.probeMedia.mockResolvedValue(206);
  intersect(h.frames()[0]!, true); await flush();
  fireEvent.error(h.container.querySelector('img')!);
  intersect(h.frames()[0]!, false); await flush();
  h.probeMedia.mockResolvedValueOnce(202).mockResolvedValue(206);
  intersect(h.frames()[0]!, true); await flush();
  expect(h.probeMedia).toHaveBeenCalledTimes(2);
  expect(h.container.querySelectorAll('img')).toHaveLength(0);
  await act(async () => { vi.advanceTimersByTime(5000); }); await flush();
  expect(h.container.querySelector('img')?.getAttribute('src')).toBe('/thumb/p0');
  expect(h.probeMedia).toHaveBeenCalledTimes(3);
});
