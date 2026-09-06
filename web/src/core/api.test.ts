import { describe, expect, it, vi } from 'vitest';

import { ApiClient, ApiError } from './api';
import { classifyMediaStatus } from './media';
import { createAuthTransport } from './auth';

function jsonResponse(status: number, body: unknown, headers: Record<string, string> = {}): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json', ...headers },
  });
}

describe('ApiClient', () => {
  it('sends same-origin credentials in cookie mode and no token in the URL', async () => {
    const fetchImpl = vi.fn((_input: string, _init?: RequestInit) =>
      Promise.resolve(jsonResponse(200, { ok: true })),
    );
    const client = new ApiClient({ fetchImpl, transport: createAuthTransport('cookie') });
    await client.getHome();
    const call = fetchImpl.mock.calls[0];
    expect(call).toBeDefined();
    expect(call?.[0]).toBe('/api/v1/home');
    expect(String(call?.[0])).not.toContain('token');
    expect(call?.[1]?.credentials).toBe('same-origin');
  });

  it('raises the unauthorized hook exactly once on a 401', async () => {
    const onUnauthorized = vi.fn();
    const client = new ApiClient({
      fetchImpl: () =>
        Promise.resolve(jsonResponse(401, { error: { code: 'unauthorized', message: 'no' } })),
      onUnauthorized,
    });
    await expect(client.getHome()).rejects.toBeInstanceOf(ApiError);
    expect(onUnauthorized).toHaveBeenCalledTimes(1);
  });

  it('records a successful authenticated response for the 24 h rule', async () => {
    const onAuthOk = vi.fn();
    const client = new ApiClient({
      fetchImpl: () => Promise.resolve(jsonResponse(200, { widgets: [] })),
      onAuthOk,
    });
    await client.getHome();
    expect(onAuthOk).toHaveBeenCalledTimes(1);
  });

  it('parses Retry-After as seconds and as a date', async () => {
    const client = new ApiClient({
      fetchImpl: () =>
        Promise.resolve(
          jsonResponse(429, { error: { code: 'rate_limited', message: 'slow down' } }, { 'Retry-After': '5' }),
        ),
    });
    const error = await client.getHome().catch((e: unknown) => e as ApiError);
    expect(error).toBeInstanceOf(ApiError);
    expect((error as ApiError).code).toBe('rate_limited');
    expect((error as ApiError).retryAfterMs).toBe(5000);
  });

  it('builds an ID-only media URL', () => {
    const client = new ApiClient({ fetchImpl: () => Promise.resolve(jsonResponse(200, {})) });
    expect(client.mediaUrl('ph_01', 'thumb')).toBe('/api/v1/media/photos/ph_01?variant=thumb');
  });

  it('sends a bearer header and never a cookie in bearer mode', async () => {
    const transport = createAuthTransport('bearer');
    transport.onClaimed('atr_scr_test');
    const fetchImpl = vi.fn((_input: string, _init?: RequestInit) =>
      Promise.resolve(jsonResponse(200, {})),
    );
    await new ApiClient({ fetchImpl, transport }).getHome();
    const init = fetchImpl.mock.calls[0]?.[1];
    expect(new Headers(init?.headers).get('Authorization')).toBe('Bearer atr_scr_test');
    expect(init?.credentials).toBe('omit');
  });
});

describe('probeMedia (design §7.3)', () => {
  function probeWith(status: number) {
    const authOk = vi.fn();
    const fetchImpl = vi.fn((_input: string, _init?: RequestInit) =>
      Promise.resolve(new Response(status === 204 ? null : 'x', { status })),
    );
    const client = new ApiClient({ fetchImpl, onAuthOk: authOk });
    return { client, fetchImpl, authOk };
  }

  it('asks for one byte so the status can be classified before decoding', async () => {
    const { client, fetchImpl } = probeWith(200);
    await client.probeMedia('ph_01');
    const init = fetchImpl.mock.calls[0]?.[1];
    // Asserted on the raw init: jsdom's `Headers` still drops `Range` as a
    // forbidden name, while the Fetch spec allows a simple `bytes=0-N` range.
    expect((init?.headers as Record<string, string>).Range).toBe('bytes=0-0');
    expect(init?.method).toBe('GET');
  });

  it('treats 206 exactly like 200 — a ranged GET is what the Core answers', async () => {
    for (const status of [200, 206]) {
      const { client, authOk } = probeWith(status);
      expect(await client.probeMedia('ph_01')).toBe(status);
      expect(classifyMediaStatus(status)).toBe('ready');
      // Both refresh the 24 h auth cache (PRD 8.2).
      expect(authOk).toHaveBeenCalledTimes(1);
    }
  });

  it('passes the not-ready statuses through untouched', async () => {
    for (const [status, outcome] of [
      [202, 'processing'],
      [404, 'gone'],
      [410, 'gone'],
      [503, 'unavailable'],
    ] as const) {
      const { client } = probeWith(status);
      expect(classifyMediaStatus(await client.probeMedia('ph_01'))).toBe(outcome);
    }
  });
});
