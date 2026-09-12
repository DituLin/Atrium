/**
 * Pair screen (W-102, design §6.8): the six-digit code at TV size plus the
 * exact command the operator runs on the Mac mini. No text input is ever
 * required on the TV.
 */

import type { ReactElement } from 'react';
import { useEffect, useRef } from 'react';
import { RemoteButton } from '../ui/RemoteButton';

import { useApp } from '../app/context';
import { usePairing } from './usePairing';

function CodeDigits(props: { code: string }): ReactElement {
  return (
    <div className="paircode" aria-label={`配对码 ${props.code.split('').join(' ')}`}>
      {props.code.split('').map((digit, index) => (
        <span className="paircode__digit" key={`${digit}-${index}`}>
          {digit}
        </span>
      ))}
    </div>
  );
}

export function PairScreen(): ReactElement {
  const { state } = useApp();
  const { restart } = usePairing();
  const pairing = state.pairing;
  const retry = useRef<HTMLButtonElement>(null);
  useEffect(() => { if (pairing.phase === 'error') retry.current?.focus(); }, [pairing.phase]);

  return (
    <div className="screen screen--pair">
      <h1 className="pair__title">连接这方屏幕</h1>
      {state.needsPairing ? <p className="pair__status">此屏幕尚未获得有效授权。请在 Mac mini 上完成配对。</p> : null}

      {pairing.phase === 'starting' || pairing.phase === 'idle' ? (
        <p className="pair__status">正在获取配对码…</p>
      ) : null}

      {pairing.code && (pairing.phase === 'waiting' || pairing.phase === 'approved') ? (
        <CodeDigits code={pairing.code} />
      ) : null}

      {pairing.phase === 'waiting' ? (
        <ol className="pair__steps">
          <li>请在 Mac mini 上确认此屏幕：</li>
          <li>
            <code className="pair__command">
              atrium admin pair approve {pairing.code} --name &quot;客厅屏幕&quot;
            </code>
          </li>
          <li>确认后会自动继续。配对码到期后会自动更新。</li>
        </ol>
      ) : null}

      {pairing.phase === 'approved' || pairing.phase === 'claiming' ? (
        <p className="pair__status">已获准连接，正在完成配对…</p>
      ) : null}

      {pairing.phase === 'claimed' ? (
        <p className="pair__status">已配对为「{pairing.screenName}」，正在读取首页…</p>
      ) : null}

      {pairing.phase === 'expired' ? (
        <p className="pair__status pair__status--warn" role="status">
          <span aria-hidden="true">⚠</span> 配对码已过期，正在获取新码…
        </p>
      ) : null}

      {pairing.phase === 'error' ? (
        <div className="pair__error" role="status">
          <p>
            <span aria-hidden="true">⚠</span> 暂时无法完成配对，系统会自动重试。请确认家庭服务可用。
          </p>
          <RemoteButton ref={retry} type="button" className="button" onClick={restart}>
            重新获取配对码
          </RemoteButton>
        </div>
      ) : null}
    </div>
  );
}
