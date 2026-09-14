import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { useEffect, useReducer } from 'react';
import { beforeEach, expect, it, vi } from 'vitest';
import { AppContext } from '../app/context';
import type { AppContextValue } from '../app/context';
import { appReducer, createInitialState } from '../app/state';
import type { AppAction } from '../app/state';
import { VideosScreen } from './VideosScreen';
import { VideoRefresh } from '../app/videoRefresh';
import { CurrentScreen } from '../App';
import type { VideoItem } from '../types/api';

const item = (id: string): VideoItem => ({ id, source_id: 'nas', revision: 1, status: 'ready', first_seen_at: '2026-09-14T00:00:00Z' });
const listVideos = vi.fn();
const getVideo = vi.fn();
const api = { listVideos, getVideo };
const videos = new VideoRefresh();
let dispatch: (action: AppAction) => void;
function Harness({ actual = false }: { actual?: boolean }) {
 const [state, send] = useReducer(appReducer, undefined, () => {
  const s = createInitialState(Date.now()); s.router.route = { name: 'videos' };
  s.connection = { ...s.connection, status: 'online', hasSnapshot: true }; return s;
 });
 useEffect(() => { dispatch = send; }, [send]);
 const context = { state, dispatch: send, api, videos, goBack: () => send({ type: 'router.back' }) } as unknown as AppContextValue;
 return <AppContext.Provider value={context}>{actual ? <CurrentScreen /> : <VideosScreen />}</AppContext.Provider>;
}
beforeEach(() => {
 vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible');
 vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue();
 vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(() => {});
 vi.spyOn(HTMLMediaElement.prototype, 'load').mockImplementation(() => {});
 listVideos.mockReset().mockResolvedValue({ items: [item('v01'), item('v02')], next_cursor: null });
 getVideo.mockReset().mockImplementation(async (id: string) => ({ item: item(id) }));
});
it('opens a card in preview and returns to its exact focus and scroll without refetching the list', async () => {
 const view = render(<Harness />);
 const card = await screen.findByRole('button', { name: '视频 2' });
 const grid = screen.getByRole('region', { name: '视频列表' }); grid.scrollTop = 280;
 fireEvent.focus(card); fireEvent.click(card);
 await screen.findByRole('button', { name: '播放' });
 expect(HTMLMediaElement.prototype.play).not.toHaveBeenCalled();
 fireEvent.click(screen.getByRole('button', { name: '返回视频' }));
 await waitFor(() => expect(document.activeElement).toBe(card));
 expect(grid.scrollTop).toBe(280);
 expect(view.container.querySelector('video')).toBeNull();
 expect(listVideos).toHaveBeenCalledTimes(1);
});
it('offers recovery for a failed list instead of showing it as empty', async () => {
 listVideos.mockRejectedValueOnce(new Error('private path'));
 render(<Harness />);
 expect(await screen.findByText('暂时无法读取视频列表')).toBeTruthy();
 expect(screen.queryByText('还没有视频')).toBeNull();
 fireEvent.click(screen.getByRole('button', { name: '重试' }));
 expect(await screen.findByRole('button', { name: '视频 1' })).toBeTruthy();
});
it('stops showing a revoked video and leaves a remotely operable return action', async () => {
 getVideo.mockRejectedValueOnce(new Error('not available'));
 render(<Harness />);
 fireEvent.click(await screen.findByRole('button', { name: '视频 1' }));
 expect(await screen.findByText('暂时无法读取此视频')).toBeTruthy();
 expect(screen.getByRole('button', { name: '返回视频' })).toBeTruthy();
});
it('discards a late detail when the user has already returned', async () => {
 let finish!: (value: { item: VideoItem }) => void;
 getVideo.mockImplementationOnce(() => new Promise(resolve => { finish = resolve; }));
 const view = render(<Harness />);
 fireEvent.click(await screen.findByRole('button', { name: '视频 1' }));
 await waitFor(() => expect(getVideo).toHaveBeenCalledTimes(1));
 act(() => dispatch({ type: 'router.back' }));
 await act(async () => { finish({ item: item('v01') }); });
 expect(view.container.querySelector('video')).toBeNull();
 expect(screen.getByRole('button', { name: '视频 1' })).toBeTruthy();
});
it('unmounts and releases the actual player when authorization expires', async () => {
 const view = render(<Harness actual />);
 fireEvent.click(await screen.findByRole('button', { name: '视频 1' }));
 fireEvent.click(await screen.findByRole('button', { name: '播放' }));
 const video = view.container.querySelector('video')!;
 expect(video.getAttribute('src')).toContain('/content');
 act(() => dispatch({ type: 'app.authExpired' }));
 expect(view.container.querySelector('video')).toBeNull();
 expect(video.getAttribute('src')).toBeNull();
 expect(HTMLMediaElement.prototype.pause).toHaveBeenCalled();
 expect(screen.getByText('需要重新确认屏幕授权', { exact: false })).toBeTruthy();
});
it('appends the next page without discarding existing cards', async () => {
 listVideos.mockResolvedValueOnce({ items: [item('v01')], next_cursor: 'next' });
 listVideos.mockResolvedValueOnce({ items: [item('v02')], next_cursor: null });
 render(<Harness />);
 const first = await screen.findByRole('button', { name: '视频 1' });
 screen.getByRole('button', { name: '加载更多' }).focus();
 fireEvent.keyDown(document.activeElement!, { key: 'Enter' });
 expect(await screen.findByRole('button', { name: '视频 2' })).toBeTruthy();
 expect(screen.getByRole('button', { name: '视频 1' })).toBe(first);
 expect(document.activeElement).toBe(screen.getByRole('button', { name: '视频 2' }));
 expect(listVideos.mock.calls[1]?.[0]).toEqual({ limit: 50, cursor: 'next' });
});

it('awaits remote refresh and updates the list while retaining the current card', async () => {
 render(<Harness />);
 const card = await screen.findByRole('button', { name: '视频 2' }); card.focus();
 listVideos.mockResolvedValueOnce({items:[item('v01'),item('v02'),item('v03')],next_cursor:null});
 await act(async () => { await videos.load({name:'videos'}); });
 expect(screen.getByRole('button',{name:'视频 3'})).toBeTruthy();
 expect(document.activeElement).toBe(card);
});
it('keeps the same playing media element when a remote detail refresh is unchanged', async () => {
 const view = render(<Harness />);
 fireEvent.click(await screen.findByRole('button',{name:'视频 1'}));
 fireEvent.click(await screen.findByRole('button',{name:'播放'}));
 const video = view.container.querySelector('video');
 await act(async () => { await videos.load({name:'video',videoId:'v01'}); });
 expect(getVideo).toHaveBeenCalledTimes(2);
 expect(view.container.querySelector('video')).toBe(video);
 expect(screen.getByRole('button',{name:'暂停'})).toBeTruthy();
});
