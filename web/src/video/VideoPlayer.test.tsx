import { act, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { VideoPlayer } from './VideoPlayer';

let completePlay: () => void;
let play: ReturnType<typeof vi.spyOn>;
let pause: ReturnType<typeof vi.spyOn>;
let load: ReturnType<typeof vi.spyOn>;
beforeEach(() => {
  vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible');
  play = vi.spyOn(HTMLMediaElement.prototype, 'play').mockImplementation(() => new Promise<void>(resolve => { completePlay = resolve; }));
  pause = vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(() => {});
  load = vi.spyOn(HTMLMediaElement.prototype, 'load').mockImplementation(() => {});
});
function mount() {
  const onBack = vi.fn();
  const view = render(<VideoPlayer id="video_01" revision={1} onBack={onBack} />);
  const video = view.container.querySelector('video')!;
  return { ...view, video, onBack };
}

describe('VideoPlayer explicit playback lifecycle', () => {
  it('opens in preview without fetching content or autoplay, and confirms once to play', () => {
    const { video } = mount();
    expect(screen.getByRole('button', { name: '播放' })).toBe(document.activeElement);
    expect(video.getAttribute('src')).toBeNull();
    expect(play).not.toHaveBeenCalled();
    fireEvent.keyDown(document.activeElement!, { key: 'OK' });
    expect(video.getAttribute('src')).toBe('/api/v1/media/videos/video_01/content');
    expect(play).toHaveBeenCalledTimes(1);
    expect(screen.getByRole('button', { name: '暂停' })).toBeTruthy();
  });
  it('cancels a pending play on a second confirm and pauses its late completion', async () => {
    mount();
    fireEvent.click(screen.getByRole('button', { name: '播放' }));
    fireEvent.click(screen.getByRole('button', { name: '暂停' }));
    const calls = pause.mock.calls.length;
    await act(async () => { completePlay(); });
    expect(pause.mock.calls.length).toBeGreaterThan(calls);
    expect(screen.getByRole('button', { name: '播放' })).toBeTruthy();
  });
  it('pauses when hidden, rejects late play, and stays paused on foreground', async () => {
    mount();
    fireEvent.click(screen.getByRole('button', { name: '播放' }));
    vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden');
    fireEvent(document, new Event('visibilitychange'));
    await act(async () => { completePlay(); });
    expect(pause).toHaveBeenCalled();
    vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible');
    fireEvent(document, new Event('visibilitychange'));
    expect(play).toHaveBeenCalledTimes(1);
    expect(screen.getByRole('button', { name: '播放' })).toBeTruthy();
  });
  it('releases media on Back before navigation and rejects late play after unmount', async () => {
    const { video, onBack, unmount } = mount();
    fireEvent.click(screen.getByRole('button', { name: '播放' }));
    fireEvent.keyDown(document.activeElement!, { key: 'GoBack' });
    expect(onBack).toHaveBeenCalledTimes(1);
    expect(video.getAttribute('src')).toBeNull();
    expect(load).toHaveBeenCalled();
    unmount();
    const calls = pause.mock.calls.length;
    await act(async () => { completePlay(); });
    expect(pause.mock.calls.length).toBeGreaterThan(calls);
  });
  it('seeks ten seconds within bounds and moves focus between playback and Back', () => {
    const { video } = mount();
    Object.defineProperty(video, 'duration', { value: 25, configurable: true });
    fireEvent.loadedMetadata(video);
    video.currentTime = 20;
    fireEvent.keyDown(document.activeElement!, { key: 'ArrowRight' });
    expect(video.currentTime).toBe(25);
    video.currentTime = 4;
    fireEvent.keyDown(document.activeElement!, { key: 'ArrowLeft' });
    expect(video.currentTime).toBe(0);
    fireEvent.keyDown(document.activeElement!, { key: 'ArrowDown' });
    expect(document.activeElement).toBe(screen.getByRole('button', { name: '返回视频' }));
    fireEvent.keyDown(document.activeElement!, { key: 'ArrowUp' });
    expect(document.activeElement).toBe(screen.getByRole('button', { name: '播放' }));
  });
  it('keeps decode failure actionable without showing media paths', () => {
    const { video } = mount();
    Object.defineProperty(video, 'error', { value: { code: 4 } });
    fireEvent.error(video);
    expect(screen.getByText('当前设备无法播放此视频')).toBeTruthy();
    expect(screen.getByRole('button', { name: '重试播放' })).toBeTruthy();
    expect(screen.getByRole('button', { name: '返回视频' })).toBeTruthy();
  });
  it('releases the previous revision and opens the replacement paused', async () => {
    const { video, rerender, onBack, container } = mount();
    fireEvent.click(screen.getByRole('button', { name: '播放' }));
    const finishOld = completePlay;
    rerender(<VideoPlayer id="video_01" revision={2} onBack={onBack} />);
    expect(video.getAttribute('src')).toBeNull();
    const replacement = container.querySelector('video')!;
    expect(replacement).not.toBe(video);
    expect(replacement.getAttribute('src')).toBeNull();
    await act(async () => { finishOld(); });
    expect(play).toHaveBeenCalledTimes(1);
    expect(screen.getByRole('button', { name: '播放' })).toBeTruthy();
  });
  it('shows a recoverable error when play rejects, then retries only on confirm', async () => {
    play.mockRejectedValueOnce(new DOMException('private media path', 'NotAllowedError'));
    mount();
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: '播放' })); });
    expect(screen.getByText('视频读取失败，请重试')).toBeTruthy();
    expect(screen.queryByText(/private media path/)).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: '重试播放' }));
    expect(play).toHaveBeenCalledTimes(2);
    expect(load).toHaveBeenCalled();
  });
  it('stops on pagehide even when the visibility signal stays visible', async () => {
    mount();
    fireEvent.click(screen.getByRole('button', { name: '播放' }));
    fireEvent(window, new Event('pagehide'));
    await act(async () => { completePlay(); });
    expect(screen.getByRole('button', { name: '播放' })).toBeTruthy();
    expect(play).toHaveBeenCalledTimes(1);
  });
  it('shows ended state and requires confirmation to replay from the start', async () => {
    const { video } = mount();
    fireEvent.click(screen.getByRole('button', { name: '播放' }));
    await act(async () => { completePlay(); });
    video.currentTime = 25;
    Object.defineProperty(video, 'ended', { value: true });
    fireEvent.ended(video);
    expect(play).toHaveBeenCalledTimes(1);
    fireEvent.click(screen.getByRole('button', { name: '重新播放' }));
    expect(video.currentTime).toBe(0);
    expect(play).toHaveBeenCalledTimes(2);
  });
  it('does not construct media requests from invalid identifiers', () => {
    const { container } = render(<VideoPlayer id="../private?token=secret" revision={1} onBack={vi.fn()} />);
    const video = container.querySelector('video')!;
    expect(video.getAttribute('poster')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: '播放' }));
    expect(play).not.toHaveBeenCalled();
    expect(video.getAttribute('src')).toBeNull();
  });
  it('does not let a queued old pause event cancel a newer play intent', async () => {
    const { video } = mount();
    fireEvent.click(screen.getByRole('button', { name: '播放' }));
    await act(async () => { completePlay(); });
    fireEvent.click(screen.getByRole('button', { name: '暂停' }));
    fireEvent.click(screen.getByRole('button', { name: '播放' }));
    Object.defineProperty(video, 'paused', { value: false });
    fireEvent.pause(video);
    await act(async () => { completePlay(); });
    expect(screen.getByRole('button', { name: '暂停' })).toBeTruthy();
  });
});

it('draws progress from the media clock and falls back to a neutral header', () => {
  const { video } = mount();
  expect(screen.getByRole('heading', { name: '家庭影像' })).toBeTruthy();
  Object.defineProperty(video, 'duration', { value: 200, configurable: true });
  fireEvent.loadedMetadata(video);
  video.currentTime = 50;
  fireEvent.timeUpdate(video);
  const bar = screen.getByRole('progressbar', { name: '播放进度' });
  expect(bar.getAttribute('aria-valuenow')).toBe('50');
  expect(bar.getAttribute('aria-valuemax')).toBe('200');
  expect((bar.querySelector('.video-player__thumb') as HTMLElement).style.left).toBe('25%');
  expect(screen.getByText('0:50')).toBeTruthy();
  expect(screen.getByText('3:20')).toBeTruthy();
});

describe('VideoPlayer native playback on the Android TV host', () => {
  function withBridge() {
    const bridge = { play: vi.fn(), stop: vi.fn() };
    (window as unknown as { AtriumNative?: unknown }).AtriumNative = bridge;
    return bridge;
  }
  const close = (detail: object) => act(() => {
    window.dispatchEvent(new CustomEvent('atrium-native-video', { detail: { positionMs: 0, durationMs: 0, ended: false, error: null, ...detail } }));
  });
  afterEach(() => { delete (window as unknown as { AtriumNative?: unknown }).AtriumNative; });

  it('hands the stream to the host instead of the web element', () => {
    const bridge = withBridge();
    const { video } = mount();
    fireEvent.keyDown(document.activeElement!, { key: 'OK' });
    expect(bridge.play).toHaveBeenCalledExactlyOnceWith('/api/v1/media/videos/video_01/content', '家庭影像', '');
    expect(play).not.toHaveBeenCalled();
    expect(video.getAttribute('src')).toBeNull();
  });

  it('shows the outcome when the host closes and returns focus to play', () => {
    withBridge();
    mount();
    fireEvent.keyDown(document.activeElement!, { key: 'OK' });
    close({ positionMs: 42_000, durationMs: 42_000, ended: true });
    expect(screen.getByRole('status').textContent).toBe('这一刻，已放映完毕');
    expect(document.activeElement).toBe(screen.getByRole('button', { name: '重新播放' }));
  });

  it('reports decoder failures as unsupported', () => {
    withBridge();
    mount();
    fireEvent.keyDown(document.activeElement!, { key: 'OK' });
    close({ error: 'ERROR_CODE_DECODING_FORMAT_UNSUPPORTED' });
    expect(screen.getByRole('status').textContent).toBe('当前设备无法播放此视频');
  });

  it('fetches a playback ticket for hosts that take one, then hands it over', async () => {
    const bridge = { play: vi.fn(), playWithTicket: vi.fn(), stop: vi.fn() };
    (window as unknown as { AtriumNative?: unknown }).AtriumNative = bridge;
    let grant!: (value: string) => void;
    const ticket = vi.fn((_id: string, _signal: AbortSignal) => new Promise<string>(resolve => { grant = resolve; }));
    render(<VideoPlayer id="video_01" revision={1} onBack={vi.fn()} ticket={ticket} />);
    fireEvent.keyDown(document.activeElement!, { key: 'OK' });
    fireEvent.keyDown(document.activeElement!, { key: 'OK' });
    expect(ticket).toHaveBeenCalledTimes(1);
    expect(ticket.mock.calls[0]?.[0]).toBe('video_01');
    expect(screen.getByRole('status').textContent).toBe('正在读取视频…');
    await act(async () => { grant('amt1.payload.sig'); });
    expect(bridge.playWithTicket).toHaveBeenCalledExactlyOnceWith('/api/v1/media/videos/video_01/content', '家庭影像', '', 'amt1.payload.sig');
    expect(bridge.play).not.toHaveBeenCalled();
    expect(play).not.toHaveBeenCalled();
  });

  it('reports a failed ticket request and never opens the host player', async () => {
    const bridge = { play: vi.fn(), playWithTicket: vi.fn(), stop: vi.fn() };
    (window as unknown as { AtriumNative?: unknown }).AtriumNative = bridge;
    render(<VideoPlayer id="video_01" revision={1} onBack={vi.fn()} ticket={() => Promise.reject(new Error('offline'))} />);
    await act(async () => { fireEvent.keyDown(document.activeElement!, { key: 'OK' }); });
    expect(screen.getByRole('status').textContent).toBe('视频读取失败，请重试');
    expect(bridge.playWithTicket).not.toHaveBeenCalled();
  });

  it('abandons a pending ticket when the page is left', async () => {
    const bridge = { play: vi.fn(), playWithTicket: vi.fn(), stop: vi.fn() };
    (window as unknown as { AtriumNative?: unknown }).AtriumNative = bridge;
    let signal!: AbortSignal;
    let grant!: (value: string) => void;
    const view = render(<VideoPlayer id="video_01" revision={1} onBack={vi.fn()}
      ticket={(_id, s) => { signal = s; return new Promise<string>(resolve => { grant = resolve; }); }} />);
    fireEvent.keyDown(document.activeElement!, { key: 'OK' });
    view.unmount();
    expect(signal.aborted).toBe(true);
    await act(async () => { grant('late'); });
    expect(bridge.playWithTicket).not.toHaveBeenCalled();
    expect(bridge.stop).not.toHaveBeenCalled();
  });

  it('stops native playback when the page is left, and ignores a stale close', () => {
    const bridge = withBridge();
    const view = mount();
    fireEvent.keyDown(document.activeElement!, { key: 'OK' });
    view.unmount();
    expect(bridge.stop).toHaveBeenCalledTimes(1);
    close({ ended: true });
  });
});
