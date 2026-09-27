import { useEffect, useRef, useState } from 'react';
import { NATIVE_VIDEO_EVENT, isDecoderError, nativeVideo } from '../core/nativeVideo';
import type { NativeVideoResult } from '../core/nativeVideo';
import { RemoteButton } from '../ui/RemoteButton';
import { mapRemoteKey } from '../ui/keys';
import type { RemoteKey } from '../ui/keys';

export interface VideoPlayerProps {
  id: string;
  revision: number;
  onBack: () => void;
  /** Header and info line; each is shown only when the caller knows it. */
  title?: string;
  subtitle?: string;
  info?: string;
  /** Fetches a playback ticket for hosts whose native player needs one. */
  ticket?: (id: string, signal: AbortSignal) => Promise<string>;
}
type Phase = 'preview' | 'loading' | 'playing' | 'paused' | 'ended' | 'error' | 'unsupported' | 'native';
interface Session { toggle: () => void; stop: () => void; release: () => void }
const PLAY = 'M8 5v14l11-7z';
const PAUSE = 'M7 5h4v14H7z M13 5h4v14h-4z';
const REPLAY = 'M12 5V2L7 6l5 4V7a6 6 0 1 1-6 6H4a8 8 0 1 0 8-8z';
const time = (seconds: number) => {
  const n = Number.isFinite(seconds) ? Math.max(0, Math.floor(seconds)) : 0;
  return `${Math.floor(n / 60)}:${String(n % 60).padStart(2, '0')}`;
};

/** A keyed session owns each video's media element and pending play promises. */
export function VideoPlayer(props: VideoPlayerProps) {
  return <PlayerSession key={`${props.id}:${props.revision}`} {...props} />;
}
function PlayerSession({ id, onBack, title, subtitle, info, ticket }: VideoPlayerProps) {
  const media = useRef<HTMLVideoElement>(null);
  const playButton = useRef<HTMLButtonElement>(null);
  const backButton = useRef<HTMLButtonElement>(null);
  const session = useRef<Session | null>(null);
  const [phase, setPhase] = useState<Phase>('preview');
  const [position, setPosition] = useState(0);
  const [duration, setDuration] = useState(0);
  const valid = /^[A-Za-z0-9_-]{1,64}$/.test(id);

  useEffect(() => {
    const video = media.current!;
    let disposed = false;
    let intent = false;
    let generation = 0;
    let failed = false;
    let nativeOpen = false;
    let ticketRequest: AbortController | null = null;
    const visible = () => document.visibilityState === 'visible';
    const stop = () => {
      intent = false;
      generation++;
      video.pause();
      if (!disposed) setPhase('paused');
    };
    const release = () => {
      ticketRequest?.abort();
      ticketRequest = null;
      if (native && nativeOpen) { nativeOpen = false; native.stop(); }
      stop();
      disposed = true;
      video.removeAttribute('src');
      video.load();
    };
    const native = nativeVideo();
    const toggle = () => {
      if (disposed || !valid || !visible()) return;
      if (native) {
        // The host covers the page with its own player until Back.
        const path = `/api/v1/media/videos/${id}/content`;
        const open = (start: () => void) => { start(); nativeOpen = true; setPhase('native'); };
        if (!native.playWithTicket || !ticket) {
          open(() => native.play(path, title ?? '家庭影像', subtitle ?? ''));
          return;
        }
        if (ticketRequest) return;
        const request = new AbortController();
        ticketRequest = request;
        setPhase('loading');
        ticket(id, request.signal).then(value => {
          if (disposed || ticketRequest !== request) return;
          ticketRequest = null;
          if (!visible()) { setPhase('paused'); return; }
          open(() => native.playWithTicket!(path, title ?? '家庭影像', subtitle ?? '', value));
        }, () => {
          if (disposed || ticketRequest !== request) return;
          ticketRequest = null;
          setPhase('error');
        });
        return;
      }
      if (intent) { stop(); return; }
      intent = true;
      const request = ++generation;
      if (failed) { video.removeAttribute('src'); video.load(); failed = false; }
      if (!video.getAttribute('src')) video.src = `/api/v1/media/videos/${id}/content`;
      if (video.ended) video.currentTime = 0;
      setPhase('loading');
      const reject = () => {
        if (disposed || request !== generation) return;
        intent = false;
        failed = true;
        video.pause();
        setPhase(video.error?.code === 4 ? 'unsupported' : 'error');
      };
      try {
        void video.play().then(() => {
          // An old request may resolve after pause, backgrounding or teardown.
          // Invalidate even a newer intent: another explicit press can safely restart.
          if (disposed || request !== generation || !intent || !visible()) {
            intent = false;
            generation++;
            video.pause();
            if (!disposed) setPhase('paused');
            return;
          }
          setPhase('playing');
        }, reject);
      } catch { reject(); }
    };
    const hidden = () => { if (!visible()) stop(); };
    const pagehide = () => stop();
    const playing = () => {
      if (disposed || !intent || !visible()) { video.pause(); return; }
      setPhase('playing');
    };
    const waiting = () => { if (intent && !disposed) setPhase('loading'); };
    const ended = () => { intent = false; generation++; if (!disposed) setPhase('ended'); };
    const error = () => {
      intent = false;
      generation++;
      failed = true;
      video.pause();
      if (!disposed) setPhase(video.error?.code === 4 ? 'unsupported' : 'error');
    };
    const paused = () => {
      // A queued pause event can arrive after a subsequent play has begun.
      if (intent && video.paused) { intent = false; generation++; if (!disposed) setPhase('paused'); }
    };
    const nativeClosed = (event: Event) => {
      if (!nativeOpen || disposed) return;
      nativeOpen = false;
      const result = (event as CustomEvent<NativeVideoResult>).detail;
      setPosition(result.positionMs / 1000);
      setDuration(result.durationMs / 1000);
      setPhase(result.error ? (isDecoderError(result.error) ? 'unsupported' : 'error') : result.ended ? 'ended' : 'paused');
      playButton.current?.focus();
    };
    window.addEventListener(NATIVE_VIDEO_EVENT, nativeClosed);
    session.current = { toggle, stop, release };
    document.addEventListener('visibilitychange', hidden);
    window.addEventListener('pagehide', pagehide);
    video.addEventListener('playing', playing);
    video.addEventListener('waiting', waiting);
    video.addEventListener('ended', ended);
    video.addEventListener('error', error);
    video.addEventListener('pause', paused);
    playButton.current?.focus();
    return () => {
      disposed = true;
      release();
      session.current = null;
      document.removeEventListener('visibilitychange', hidden);
      window.removeEventListener('pagehide', pagehide);
      window.removeEventListener(NATIVE_VIDEO_EVENT, nativeClosed);
      video.removeEventListener('playing', playing);
      video.removeEventListener('waiting', waiting);
      video.removeEventListener('ended', ended);
      video.removeEventListener('error', error);
      video.removeEventListener('pause', paused);
    };
    // Title, subtitle and the ticket loader are read at press time.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id, valid]);

  const back = () => { session.current?.release(); onBack(); };
  const direction = (key: RemoteKey) => {
    if (key === 'up') playButton.current?.focus();
    else if (key === 'down') backButton.current?.focus();
    else if (key === 'left' || key === 'right') {
      const video = media.current!;
      if (Number.isFinite(video.duration) && video.duration > 0) {
        try {
          video.currentTime = Math.max(0, Math.min(video.duration, video.currentTime + (key === 'right' ? 10 : -10)));
          setPosition(video.currentTime);
        } catch { /* Metadata may be replaced during an in-flight seek. */ }
      }
    }
  };
  const active = phase === 'playing' || phase === 'loading';
  const label = active ? '暂停' : phase === 'ended' ? '重新播放' : phase === 'error' || phase === 'unsupported' ? '重试播放' : '播放';
  const message = !valid ? '视频不可用' : {
    preview: '按确认，展开这一刻', loading: '正在读取视频…', playing: '', paused: '已暂停',
    ended: '这一刻，已放映完毕', error: '视频读取失败，请重试', unsupported: '当前设备无法播放此视频',
    native: '正在播放…',
  }[phase];
  const known = Number.isFinite(duration) && duration > 0;
  const percent = known ? Math.min(100, Math.max(0, (position / duration) * 100)) : 0;
  return <section className="video-player surface--ink" aria-label="视频预览" onKeyDown={event => {
    if (mapRemoteKey(event) === 'back') {
      event.preventDefault(); event.stopPropagation();
      if (!event.repeat) back();
    }
  }}>
    <video ref={media} playsInline preload="none" poster={valid ? `/api/v1/media/videos/${id}/cover` : undefined}
      onLoadedMetadata={() => setDuration(media.current!.duration)}
      onDurationChange={() => setDuration(media.current!.duration)}
      onTimeUpdate={() => setPosition(media.current!.currentTime)} />
    <header className="video-player__header">
      <div className="video-player__heading">
        <h1 className="video-player__title serif">{title ?? '家庭影像'}</h1>
        {subtitle ? <p className="video-player__subtitle">{subtitle}</p> : null}
      </div>
      <p className="video-player__volume">音量请用遥控器调节</p>
    </header>
    {message && <p className="video-player__status serif" role="status">{message}</p>}
    <div className="video-player__scrim" aria-hidden="true" />
    <div className="video-player__controls">
      <div className="video-player__timeline">
        <span className="video-player__time">{time(position)}</span>
        <div className="video-player__track" role="progressbar" aria-label="播放进度"
          aria-valuemin={0} aria-valuemax={known ? Math.floor(duration) : 0} aria-valuenow={Math.floor(position)}>
          <span className="video-player__played" style={{ width: `${percent}%` }} />
          <span className="video-player__thumb" style={{ left: `${percent}%` }} />
        </div>
        <span className="video-player__time video-player__time--end">{time(duration)}</span>
      </div>
      <div className="video-player__actions">
        <p className="video-player__hint">← → 跳转 10 秒 · OK {label} · ↓ 返回按钮</p>
        <RemoteButton ref={playButton} className="video-player__play" aria-label={label} disabled={!valid}
          onClick={() => session.current?.toggle()} onDirection={direction}>
          <svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d={active ? PAUSE : phase === 'ended' ? REPLAY : PLAY} /></svg>
        </RemoteButton>
        <div className="video-player__end">
          {info ? <span className="video-player__info">{info}</span> : null}
          <RemoteButton ref={backButton} className="btn btn--ghost-ink" onClick={back} onDirection={direction}>返回视频</RemoteButton>
        </div>
      </div>
    </div>
  </section>;
}
