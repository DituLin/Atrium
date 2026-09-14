import { describe, expect, it, vi } from 'vitest';
import { ApiClient } from './api';
import { createAuthTransport } from './auth';

const response = () => new Response(JSON.stringify({ items: [], next_cursor: null }), { status: 200 });
describe('video API transport', () => {
  it('encodes pagination and uses authenticated no-store requests', async () => {
    const fetchImpl = vi.fn((_path: string, _init?: RequestInit) => Promise.resolve(response()));
    const client = new ApiClient({ fetchImpl, transport: createAuthTransport('cookie') });
    await client.listVideos({ cursor: 'a+b/=', limit: 50 });
    const [url, init] = fetchImpl.mock.calls[0]!;
    expect(url).toBe('/api/v1/videos?limit=50&cursor=a%2Bb%2F%3D');
    expect(init?.credentials).toBe('same-origin');
    expect(init?.cache).toBe('no-store');
  });
  it('ignores a late detail response after cancellation', async () => {
    let finish!: (r: Response) => void;
    const signal = new AbortController();
    const client = new ApiClient({ fetchImpl: () => new Promise(resolve => { finish = resolve; }) });
    const request = client.getVideo('v01', signal.signal);
    signal.abort();
    finish(new Response(JSON.stringify({ item: { id: 'v01' } })));
    await expect(request).rejects.toMatchObject({ name: 'AbortError' });
  });
  it('invalidates pending video data when credentials change', async () => {
    let finish!: (r: Response) => void;
    const client = new ApiClient({ fetchImpl: () => new Promise(resolve => { finish = resolve; }) });
    const request = client.listVideos({});
    client.invalidateAuthorization();
    finish(response());
    await expect(request).rejects.toMatchObject({ name: 'AbortError' });
  });
  it('propagates screen revocation to the app', async () => {
    const onUnauthorized = vi.fn();
    const client = new ApiClient({ onUnauthorized, fetchImpl: async () => new Response(JSON.stringify({ error: { code: 'screen_revoked' } }), { status: 410 }) });
    await expect(client.getVideo('v01')).rejects.toMatchObject({ code: 'screen_revoked' });
    expect(onUnauthorized).toHaveBeenCalledTimes(1);
  });
});
