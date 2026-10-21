import { expect, it, vi } from 'vitest';
import { ApiClient } from './api';
import { createAuthTransport } from './auth';

const overview = { generation: '1:4', total: 12, confirmed: 10, pending: 2, domains: [], entities: [], ownership: { household: 2, unassigned: 1, waiting: 1 }, coverage: { complete: true } };
const response = () => new Response(JSON.stringify({ schema_version: '1', availability: 'available', data: overview }));
it('reads the whole public archive overview with screen authentication and no browser cache', async () => {
  const fetchImpl = vi.fn((_url: string, _init?: RequestInit) => Promise.resolve(response()));
  const client = new ApiClient({ fetchImpl, transport: createAuthTransport('cookie') });
  const signal = new AbortController();
  expect(await client.getArchiveOverview(signal.signal)).toEqual(overview);
  const [url, init] = fetchImpl.mock.calls[0]!;
  expect(url).toBe('/api/v1/archives/overview');
  expect(init).toMatchObject({ method: 'GET', cache: 'no-store', credentials: 'same-origin', signal: signal.signal });
});
it('does not publish an overview from an expired screen authorization', async () => {
  let finish!: (response: Response) => void;
  const client = new ApiClient({ fetchImpl: () => new Promise(resolve => { finish = resolve; }) });
  const request = client.getArchiveOverview();
  client.invalidateAuthorization();
  finish(response());
  await expect(request).rejects.toMatchObject({ name: 'AbortError' });
});
