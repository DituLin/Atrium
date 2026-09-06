/**
 * Collection browser and viewer, rendered for real (W-202, W-203): remote keys
 * move the focus, Enter opens the photo, and an empty `recent` explains that
 * the baseline import is not "new".
 */

import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import type { ReactElement } from 'react';
import { useEffect } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { CurrentScreen } from '../App';
import { AppProvider } from '../app/AppProvider';
import { useApp } from '../app/context';
import type { AppRoute } from '../app/router';
import type { HomeResponse, PhotoItem } from '../types/api';

const HOME: HomeResponse = {
  schema_version: 1,
  server_time: new Date().toISOString(),
  version: 1,
  home: { name: 'Demo Home', timezone: 'Asia/Singapore' },
  widgets: [
    {
      type: 'clock',
      payload: {
        timezone: 'Asia/Singapore',
        server_time: new Date().toISOString(),
        utc_offset_seconds: 28800,
      },
    },
  ],
};

function photo(index: number): PhotoItem {
  const id = `ph_${index}`;
  return {
    id,
    source_id: 'family_photos',
    captured_at: index === 0 ? null : `2026-09-0${(index % 5) + 1}T10:15:00+08:00`,
    captured_confidence: index === 0 ? 'unknown' : index === 1 ? 'inferred' : 'exact',
    first_seen_at: '2026-09-05T02:00:00Z',
    is_baseline: false,
    width: 1600,
    height: 1200,
    preview: { status: 'ready', width: 1280, height: 960 },
    urls: { preview: `/api/v1/media/photos/${id}?variant=preview`, thumb: `/t/${id}` },
  };
}

class StubSocket {
  onopen: ((e: unknown) => void) | null = null;
  onclose: ((e: unknown) => void) | null = null;
  onerror: ((e: unknown) => void) | null = null;
  onmessage: ((e: unknown) => void) | null = null;
  send(): void {}
  close(): void {}
}

function stubApi(items: PhotoItem[]): void {
  vi.stubGlobal('WebSocket', StubSocket);
  vi.stubGlobal(
    'fetch',
    vi.fn((input: string) => {
      let body: unknown = HOME;
      if (input.startsWith('/api/v1/photos?')) {
        body = { items, next_cursor: null, meta: { unknown_captured_count: 2 } };
      } else if (input.startsWith('/api/v1/photos/')) {
        body = {
          item: items[0],
          neighbors: { previous_id: null, next_id: items[1]?.id ?? null },
        };
      } else if (input.startsWith('/api/v1/media/')) {
        return Promise.resolve(new Response('', { status: 200 }));
      }
      return Promise.resolve(
        new Response(JSON.stringify(body), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      );
    }),
  );
}

function Harness(props: { route: AppRoute }): ReactElement {
  const { dispatch } = useApp();
  useEffect(() => {
    dispatch({ type: 'router.navigate', route: props.route });
  }, [dispatch, props.route]);
  return <CurrentScreen />;
}

function renderAt(route: AppRoute): void {
  render(
    <AppProvider>
      <Harness route={route} />
    </AppProvider>,
  );
}

beforeEach(() => {
  vi.unstubAllGlobals();
});

describe('collection browser (W-202)', () => {
  it('renders thumbs with captions and moves the focus with the remote', async () => {
    stubApi(Array.from({ length: 8 }, (_, i) => photo(i)));
    renderAt({ name: 'photos', collection: 'captured_today' });

    await waitFor(() => expect(screen.getAllByRole('button')).toHaveLength(8));
    expect(screen.getByText('Taken today')).toBeDefined();
    // captured_today carries the "not classified" count from `meta`.
    expect(screen.getByText(/2 photos have no capture time/)).toBeDefined();
    expect(screen.getByText('Date unknown')).toBeDefined();
    expect(screen.getAllByText('estimated').length).toBeGreaterThan(0);

    const first = screen.getAllByRole('button')[0] as HTMLElement;
    await waitFor(() => expect(document.activeElement).toBe(first));
    fireEvent.keyDown(first, { key: 'ArrowRight' });
    await waitFor(() =>
      expect(document.activeElement).toBe(screen.getAllByRole('button')[1] as HTMLElement),
    );
    // Down moves a whole row (5 columns), and never past the last item.
    fireEvent.keyDown(document.activeElement as HTMLElement, { key: 'ArrowDown' });
    await waitFor(() =>
      expect(document.activeElement).toBe(screen.getAllByRole('button')[6] as HTMLElement),
    );
  });

  it('opens the viewer on Enter and reports the render signal', async () => {
    stubApi(Array.from({ length: 3 }, (_, i) => photo(i)));
    renderAt({ name: 'photos', collection: 'recent' });

    await waitFor(() => expect(screen.getAllByRole('button')).toHaveLength(3));
    fireEvent.keyDown(screen.getAllByRole('button')[0] as HTMLElement, { key: 'Enter' });
    await waitFor(() => expect(screen.getByText(/Preparing this photo/)).toBeDefined());

    const image = document.querySelector('.viewer__image') as HTMLImageElement;
    expect(image.getAttribute('src')).toContain('/api/v1/media/photos/ph_0');
    fireEvent.load(image);
    await waitFor(() => expect(screen.queryByText(/Preparing this photo/)).toBeNull());
  });

  it('explains an empty recent collection and offers the slideshow', async () => {
    stubApi([]);
    renderAt({ name: 'photos', collection: 'recent' });
    await waitFor(() => expect(screen.getByText(/Nothing new since the first/)).toBeDefined());
    expect(screen.getByText('Go to slideshow')).toBeDefined();
  });
});
