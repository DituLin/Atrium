/**
 * Connect screen (W-103): shown when there is nothing cached to display, when
 * the 24 h authorization cache expired (PRD 8.2), or when the server closed the
 * session with `4003 superseded`.
 */

import type { ReactElement } from 'react';
import { useCallback, useEffect, useRef } from 'react';

import { RemoteButton } from '../ui/RemoteButton';
import { useApp } from '../app/context';

export interface ConnectScreenProps {
  onRetry?: () => void;
}

export function ConnectScreen(props: ConnectScreenProps): ReactElement {
  const { state, dispatch, api } = useApp();
  const { connection, authExpired } = state;

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

  return (
    <div className="screen screen--connect">
      <h1 className="connect__title">Atrium</h1>

      {superseded ? (
        <>
          <p className="connect__lead" role="status">
            <span aria-hidden="true">■</span> 此屏幕已在另一处连接
          </p>
          <p className="connect__detail">
            同一设备的新连接已接替此页面。关闭另一处页面后，可在这里重新连接。
          </p>
        </>
      ) : authExpired ? (
        <>
          <p className="connect__lead" role="status">
            <span aria-hidden="true">▲</span> 需要重新确认屏幕授权
          </p>
          <p className="connect__detail">
            超过 24 小时未成功确认授权，照片已隐藏。连接家庭服务并确认授权后才能继续展示。
          </p>
        </>
      ) : (
        <>
          <p className="connect__lead" role="status">
            <span aria-hidden="true">○</span> 正在连接家庭服务
          </p>
          <p className="connect__detail">
            正在等待局域网中的 Atrium Core，系统会自动重试。连接成功前不会展示家庭内容。
          </p>
        </>
      )}

      <dl className="connect__facts">
        <div>
          <dt>连接</dt>
          <dd>{{ idle: '尚未连接', connecting: '正在连接', online: '已连接', reconnecting: '正在重连', offline: '暂时离线' }[connection.status]}</dd>
        </div>
        <div>
          <dt>连续失败</dt>
          <dd>{connection.failures}</dd>
        </div>
        {connection.lastCloseCode !== null ? (
          <div>
            <dt>上次断开代码</dt>
            <dd>{connection.lastCloseCode}</dd>
          </div>
        ) : null}
      </dl>

      <RemoteButton ref={buttonRef} type="button" className="button" onClick={retry}>
        {superseded ? '重新连接此屏幕' : '立即重试'}
      </RemoteButton>
    </div>
  );
}
