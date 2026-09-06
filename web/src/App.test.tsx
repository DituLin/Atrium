/**
 * Smoke test for the whole shell: provider, connection wiring, screen switch
 * and the dashboard render, with the network and the socket stubbed.
 */

import { render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { App } from './App';
import type { HomeResponse } from './types/api';

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
    {
      type: 'photo',
      payload: {
        slideshow_interval_seconds: 30,
        totals: { ready: 0, pending_preview: 0, unsupported: 0 },
        new_today: 0,
        captured_today: 0,
        unknown_captured: 0,
        baseline: { status: 'none', completed_at: null },
        index: { state: 'idle', progress: { seen: 0, indexed: 0, pending_preview: 0 }, last_scan_at: null },
      },
    },
    { type: 'nas', payload: { sources: [] } },
    { type: 'experimental_ticker', payload: { text: 'must not render' } },
  ],
};

class StubSocket {
  onopen: ((e: unknown) => void) | null = null;
  onclose: ((e: unknown) => void) | null = null;
  onerror: ((e: unknown) => void) | null = null;
  onmessage: ((e: unknown) => void) | null = null;
  send(): void {}
  close(): void {}
}

beforeEach(() => {
  vi.stubGlobal('WebSocket', StubSocket);
});

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

/** Routes by path so the slideshow's `/photos` call does not receive `/home`. */
function stubApi(): void {
  vi.stubGlobal(
    'fetch',
    vi.fn((input: string) => {
      if (input.startsWith('/api/v1/photos')) {
        return Promise.resolve(jsonResponse({ items: [], next_cursor: null, meta: {} }));
      }
      return Promise.resolve(jsonResponse(HOME));
    }),
  );
}

describe('App shell', () => {
  it('renders the dashboard once /home succeeds and hides unknown widgets', async () => {
    stubApi();
    render(<App />);
    await waitFor(() => expect(screen.getByLabelText('Clock')).toBeDefined());
    // The slideshow asks for a seeded `random` round and reports the empty
    // library instead of showing a black rectangle (W-201, FR-03).
    await waitFor(() => expect(screen.getByText('No photos yet')).toBeDefined());
    const calls = (globalThis.fetch as unknown as { mock: { calls: string[][] } }).mock.calls;
    expect(calls.some(([url]) => /collection=random&limit=50&seed=\w+/.test(url ?? ''))).toBe(
      true,
    );
    expect(screen.getByLabelText('System status')).toBeDefined();
    expect(screen.queryByText('must not render')).toBeNull();
  });

  it('shows the pair screen on a 401', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() =>
        Promise.resolve(
          new Response(JSON.stringify({ error: { code: 'unauthorized', message: 'no' } }), {
            status: 401,
            headers: { 'Content-Type': 'application/json' },
          }),
        ),
      ),
    );
    render(<App />);
    await waitFor(() => expect(screen.getByText('Pair this screen')).toBeDefined());
  });
});
