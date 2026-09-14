import { useEffect, useRef, useState } from 'react';
import { RemoteButton } from '../ui/RemoteButton';
import { mapRemoteKey } from '../ui/keys';
import type { RemoteKey } from '../ui/keys';
import './video-player.css';

export interface VideoPlayerProps {
  id: string;
  revision: number;
  onBack: () => void;
}
type Phase = 'preview' | 'loading' | 'playing' | 'paused' | 'ended' | 'error' | 'unsupported';
interface Session { toggle: () => void; stop: () => void; release: () => void }
const time = (seconds: number) => {
  const n = Number.isFinite(seconds) ? Math.max(0, Math.floor(seconds)) : 0;
  return `${Math.floor(n / 60)}:${String(n % 60).padStart(2, '0')}`;
};

/** A keyed session owns each video's media element and pending play promises. */
export function VideoPlayer(props: VideoPlayerProps) {
  return <PlayerSession key={`${props.id}:${props.revision}`} {...props} />;
}
function PlayerSession({ id, onBack }: VideoPlayerProps) {
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
    const visible = () => document.visibilityState === 'visible';
    const stop = () => {
      intent = false;
      generation++;
      video.pause();
      if (!disposed) setPhase('paused');
    };
    const release = () => {
      stop();
      disposed = true;
      video.removeAttribute('src');
      video.load();
    };
    const toggle = () => {
      if (disposed || !valid || !visible()) return;
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
      video.removeEventListener('playing', playing);
      video.removeEventListener('waiting', waiting);
      video.removeEventListener('ended', ended);
      video.removeEventListener('error', error);
      video.removeEventListener('pause', paused);
    };
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
  }[phase];
  return <section className="video-player" aria-label="视频预览" onKeyDown={event => {
    if (mapRemoteKey(event) === 'back') {
      event.preventDefault(); event.stopPropagation();
      if (!event.repeat) back();
    }
  }}>
    <video ref={media} playsInline preload="none" poster={valid ? `/api/v1/media/videos/${id}/cover` : undefined}
      onLoadedMetadata={() => setDuration(media.current!.duration)}
      onDurationChange={() => setDuration(media.current!.duration)}
      onTimeUpdate={() => setPosition(media.current!.currentTime)} />
    <div className="video-player__caption"><span>ATRIUM · 家庭影像</span><h1>光阴有声</h1></div>
    {message && <p className="video-player__status" role="status">{message}</p>}
    <div className="video-player__controls">
      <div className="video-player__timeline">
        <span>{time(position)}</span>
        <progress aria-label="播放进度" max={Number.isFinite(duration) && duration > 0 ? duration : 1} value={position} />
        <span>{time(duration)}</span>
      </div>
      <div className="video-player__actions">
        <RemoteButton ref={playButton} disabled={!valid} onClick={() => session.current?.toggle()} onDirection={direction}>{label}</RemoteButton>
        <span className="video-player__hint">左右跳转 10 秒 · 上下选择</span>
        <RemoteButton ref={backButton} onClick={back} onDirection={direction}>返回视频</RemoteButton>
      </div>
    </div>
  </section>;
}
