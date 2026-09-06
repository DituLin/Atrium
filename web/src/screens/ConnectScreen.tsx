/**
 * Connect screen (W-103): shown when there is nothing cached to display, when
 * the 24 h authorization cache expired (PRD 8.2), or when the server closed the
 * session with `4003 superseded`.
 */

import type { ReactElement } from 'react';
import { useCallback, useEffect, useRef } from 'react';

import { useApp } from '../app/context';

export interface ConnectScreenProps {
  onRetry?: () => void;
}

export function ConnectScreen(props: ConnectScreenProps): ReactElement {
  const { state, dispatch } = useApp();
  const { connection, authExpired } = state;

  const superseded = connection.stopReason === 'superseded';

  // `4003 superseded` stops the reconnect loop on purpose (design §7.2), so
  // this screen is the only way back: a manual retry bumps `retryNonce` and the
  // connection effect builds a fresh client.
  const onRetry = props.onRetry;
  const retry = useCallback(() => {
    if (onRetry) onRetry();
    else dispatch({ type: 'connection.retry' });
  }, [dispatch, onRetry]);

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
            <span aria-hidden="true">■</span> Another session is active
          </p>
          <p className="connect__detail">
            This screen was replaced by a newer connection from the same device. Close the other
            tab or window, then reload this one.
          </p>
        </>
      ) : authExpired ? (
        <>
          <p className="connect__lead" role="status">
            <span aria-hidden="true">▲</span> Authorization check needed
          </p>
          <p className="connect__detail">
            Photos are hidden until Atrium Core confirms this screen again. Nothing is shown from
            the local cache after 24 hours without a successful check.
          </p>
        </>
      ) : (
        <>
          <p className="connect__lead" role="status">
            <span aria-hidden="true">○</span> Connecting to Atrium Core
          </p>
          <p className="connect__detail">
            Waiting for the Core on the home network. This screen retries on its own; no household
            content is shown until it succeeds.
          </p>
        </>
      )}

      <dl className="connect__facts">
        <div>
          <dt>Link</dt>
          <dd>{connection.status}</dd>
        </div>
        <div>
          <dt>Attempts</dt>
          <dd>{connection.failures}</dd>
        </div>
        {connection.lastCloseCode !== null ? (
          <div>
            <dt>Last close</dt>
            <dd>{connection.lastCloseCode}</dd>
          </div>
        ) : null}
      </dl>

      <button ref={buttonRef} type="button" className="button" onClick={retry}>
        {superseded ? 'Take over this screen' : 'Retry now'}
      </button>
    </div>
  );
}
