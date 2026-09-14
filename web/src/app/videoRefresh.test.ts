import { expect, it, vi } from 'vitest';
import { VideoRefresh } from './videoRefresh';
import { createRefetchers } from './refetchers';
import { createInitialState } from './state';
import type { ApiClient } from '../core/api';
it('awaits the active video refresh through the command refetcher', async () => {
 const videos = new VideoRefresh();
 let finish!: () => void;
 const run = vi.fn(() => new Promise<void>(resolve => { finish = resolve; }));
 videos.register({ name: 'videos' }, run);
 const getHome = vi.fn();
 const refetch = createRefetchers({ videos, api: { getHome } as unknown as ApiClient, dispatch: vi.fn(), getState: () => createInitialState(0) });
 let complete = false;
 const pending = refetch.route({ name: 'videos' }).then(() => { complete = true; });
 await Promise.resolve();
 expect(run).toHaveBeenCalledOnce(); expect(complete).toBe(false);
 finish(); await pending; expect(complete).toBe(true); expect(getHome).not.toHaveBeenCalled();
});
it('refuses stale routes and detaches only the owning registration', async () => {
 const videos = new VideoRefresh();
 const old = videos.register({ name: 'videos' }, async () => {});
 const run = vi.fn(async () => {});
 const detach = videos.register({ name: 'video', videoId: 'v02' }, run);
 old();
 await videos.load({ name: 'video', videoId: 'v02' });
 expect(run).toHaveBeenCalledOnce();
 await expect(videos.load({ name: 'video', videoId: 'v01' })).rejects.toThrow();
 detach(); await expect(videos.load({ name: 'video', videoId: 'v02' })).rejects.toThrow();
});
