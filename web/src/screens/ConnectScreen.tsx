/**
 * Connect screen (W-103): shown when there is nothing cached to display, when
 * the 24 h authorization cache expired (PRD 8.2), or when the server closed the
 * session with `4003 superseded`.
 */

import type { ReactElement } from 'react';
import { useCallback, useEffect, useState, useRef } from 'react';

import { RemoteButton } from '../ui/RemoteButton';
import { useApp } from '../app/context';
import { clockOptions, clockWidget } from '../app/homeSelect';
import { formatClock, serverNow } from '../core/clock';

export interface ConnectScreenProps {
  onRetry?: () => void;
}

export function ConnectScreen(props: ConnectScreenProps): ReactElement {
  const { state, dispatch, api, house } = useApp();
  const { connection, authExpired } = state;

  const [notice, setNotice] = useState('');
  const houseButton = useRef<HTMLButtonElement>(null);
  const entering = useRef(false);
  const mounted = useRef(true);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; }; }, []);
  const openHouse = async () => {
    if (entering.current) return;
    entering.current = true; setNotice('正在确认屏幕授权…');
    try {
      await house.load(true);
      if (mounted.current) dispatch({ type: 'router.navigate', route: { name: 'house' } });
    } catch { if (mounted.current) setNotice('暂时无法读取中枢状态'); }
    finally { entering.current = false; }
  };

  const superseded = connection.stopReason === 'superseded';

  // `4003 superseded` stops the reconnect loop on purpose (design §7.2), so
  // this screen is the only way back: a manual retry bumps `retryNonce` and the
  // connection effect builds a fresh client.
  const onRetry = props.onRetry;
  const retry = useCallback(() => {
    if (onRetry) onRetry();
    else {
      dispatch({ type: 'connection.retry' });
      // The expired-cache rule stops the socket. A manual authenticated check
      // can restart it; cached photos remain hidden until this succeeds.
      if (authExpired) void api.getHome().then(home => {
        dispatch({ type: 'app.homeLoaded', home, receivedAt: Date.now() });
      }).catch(() => { /* Keep the truthful connect/pair state on failure. */ });
    }
  }, [dispatch, onRetry, authExpired, api]);

  // The remote has no pointer: the retry button takes focus and Enter fires it.
  const buttonRef = useRef<HTMLButtonElement | null>(null);
  useEffect(() => {
    buttonRef.current?.focus();
  }, [superseded]);

  const clock = clockWidget(state.home);
  const formatted = clock ? formatClock(serverNow(state.clock, state.nowMs), clockOptions(state.home)) : null;
  const statusWord = { idle: '尚未连接', connecting: '正在连接', online: '已连接', reconnecting: '正在重连', offline: '暂时离线' }[connection.status];
  const lead = superseded ? '此屏幕已在另一处连接' : authExpired ? '需要重新确认屏幕授权' : '正在连接家庭服务';
  const attempt = superseded ? '另一会话已接管' : connection.failures > 0
    ? `已连续失败 ${connection.failures} 次 · 系统会自动重试` : `${statusWord} · 系统会自动重试`;

  return (
    <div className="info-connect surface--ink">
      <div className="info-connect__pill" role="status">
        <svg className="info-connect__icon" viewBox="0 0 24 24" aria-hidden="true"><path d="M2 8.5a15 15 0 0 1 20 0 M5 12a10.5 10.5 0 0 1 14 0 M8.5 15.5a5.5 5.5 0 0 1 7 0 M12 19h.01 M3 3l18 18" /></svg>
        <span className="info-connect__lead">{lead}</span>
        <span className="info-connect__rule" aria-hidden="true" />
        <span className="info-connect__attempt">{attempt}</span>
      </div>

      <section className="info-connect__time">
        <div className="info-connect__brand serif">ATRIUM</div>
        {formatted ? <>
          <div className="info-connect__date serif">{formatted.date}</div>
          <div className="info-connect__clock serif">{formatted.time}</div>
          <div className="info-connect__note">时间由电视继续走时，恢复连接后自动校准</div>
        </> : null}
      </section>

      <section className="info-connect__panel">
        <h1 className="info-connect__title serif">{superseded ? '另一处页面接替了此屏幕' : authExpired ? '照片已隐藏' : '连接成功前不展示家庭内容'}</h1>
        <p className="info-connect__detail">
          {superseded ? '同一设备的新连接已接替此页面。关闭另一处页面后，可在这里重新连接。'
            : authExpired ? '超过 24 小时未成功确认授权，照片已隐藏。连接家庭服务并确认授权后才能继续展示。'
              : '正在等待局域网中的 Atrium Core，系统会自动重试。连接成功前不会展示家庭内容。'}
        </p>
        <dl className="info-connect__facts">
          <div><dt>连接</dt><dd>{statusWord}</dd></div>
          <div><dt>连续失败</dt><dd>{connection.failures}</dd></div>
          {connection.lastCloseCode !== null ? <div><dt>上次断开代码</dt><dd>{connection.lastCloseCode}</dd></div> : null}
        </dl>
        <div className="info-connect__actions">
          <RemoteButton ref={buttonRef} type="button" className="btn btn--moon" onClick={retry} onDirection={key => { if (key === 'down' || key === 'right') houseButton.current?.focus(); }}>
            {superseded ? '重新连接此屏幕' : '立即重试'}
          </RemoteButton>
          {!superseded ? <RemoteButton ref={houseButton} className="btn btn--ghost-ink" onClick={() => { void openHouse(); }}
            onDirection={key => { if (key === 'up' || key === 'left') buttonRef.current?.focus(); }}>查看中枢状态</RemoteButton> : null}
        </div>
        {notice ? <p className="info-connect__notice" role="status">{notice}</p> : null}
      </section>
    </div>
  );
}
