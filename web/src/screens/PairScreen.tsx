/**
 * Pair screen (W-102, design §6.8): the six-digit code at TV size plus the
 * exact command the operator runs on the Mac mini. No text input is ever
 * required on the TV.
 */

import type { ReactElement } from 'react';
import { useEffect, useRef, useState } from 'react';
import { RemoteButton } from '../ui/RemoteButton';

import { useApp } from '../app/context';
import { usePairing } from './usePairing';

function CodeDigits(props: { code: string }): ReactElement {
  const half = Math.ceil(props.code.length / 2);
  return (
    <div className="info-pair__code serif" aria-label={`配对码 ${props.code.split('').join(' ')}`}>
      <span aria-hidden="true">{props.code.slice(0, half)}</span>
      <span className="info-pair__code-gap" aria-hidden="true" />
      <span aria-hidden="true">{props.code.slice(half)}</span>
    </div>
  );
}

function countdown(ms: number): string {
  const seconds = Math.max(0, Math.ceil(ms / 1000));
  const rest = seconds % 60;
  return `${Math.floor(seconds / 60)}:${rest < 10 ? '0' : ''}${rest}`;
}

export function PairScreen(): ReactElement {
  const { state } = useApp();
  const { restart } = usePairing();
  const pairing = state.pairing;
  const retry = useRef<HTMLButtonElement>(null);
  useEffect(() => { if (pairing.phase === 'error') retry.current?.focus(); }, [pairing.phase]);

  const expiry = pairing.expiresAt ? Date.parse(pairing.expiresAt) : NaN;
  const remaining = Number.isNaN(expiry) ? null : expiry - state.nowMs;
  // The code's lifetime is measured from when this screen first saw it.
  const [lifetime, setLifetime] = useState<{ code: string | null; ms: number }>({ code: null, ms: 0 });
  if (pairing.code !== lifetime.code) setLifetime({ code: pairing.code, ms: remaining ?? 0 });
  const progress = remaining !== null && lifetime.ms > 0 ? Math.max(0, Math.min(1, remaining / lifetime.ms)) : null;
  const showCode = pairing.code && (pairing.phase === 'waiting' || pairing.phase === 'approved');

  return (
    <div className="info-pair">
      <section className="info-pair__intro">
        <div className="info-pair__brand"><span className="serif">ATRIUM</span><span className="serif muted">中庭</span></div>
        <h1 className="info-pair__title serif">把这台电视<br />加入家里</h1>
        {state.needsPairing ? <p className="info-pair__status">此屏幕尚未获得有效授权。请在 Mac mini 上完成配对。</p> : null}
        <ol className="info-pair__steps">
          <li><span className="info-pair__step serif">一</span><span>在 Mac mini 上打开终端</span></li>
          <li><span className="info-pair__step serif">二</span><span>输入 <code className="info-pair__command">
            atrium admin pair approve {showCode ? pairing.code : '······'} --name &quot;客厅屏幕&quot;
          </code></span></li>
          <li><span className="info-pair__step serif">三</span><span>批准后，这里会自动进入首页</span></li>
        </ol>
        <p className="info-pair__foot muted">只有在 Mac mini 上批准的电视才能看到家里的照片。</p>
      </section>

      <section className="info-pair__panel">
        <div className="eyebrow">配对码</div>
        {showCode ? <CodeDigits code={pairing.code!} /> : null}

        {pairing.phase === 'starting' || pairing.phase === 'idle' ? <p className="info-pair__status">正在获取配对码…</p> : null}

        {pairing.phase === 'waiting' ? (
          <>
            {remaining !== null ? (
              <div className="info-pair__timer">
                <div className="info-pair__track"><div className="info-pair__bar" style={{ width: `${Math.round((progress ?? 1) * 100)}%` }} /></div>
                <span className="info-pair__left" aria-label="配对码剩余时间">{countdown(remaining)}</span>
              </div>
            ) : null}
            <p className="muted">确认后会自动继续。过期后会自动换一个新码，不需要任何操作。</p>
          </>
        ) : null}

        {pairing.phase === 'approved' || pairing.phase === 'claiming' ? <p className="info-pair__status">已获准连接，正在完成配对…</p> : null}

        {pairing.phase === 'claimed' ? <p className="info-pair__status">已配对为「{pairing.screenName}」，正在读取首页…</p> : null}

        {pairing.phase === 'expired' ? (
          <p className="info-pair__status info-pair__status--warn" role="status">配对码已过期，正在获取新码…</p>
        ) : null}

        {pairing.phase === 'error' ? (
          <div className="info-pair__error" role="status">
            <p className="info-pair__status info-pair__status--warn">暂时无法完成配对，系统会自动重试。请确认家庭服务可用。</p>
            <RemoteButton ref={retry} type="button" className="btn btn--primary" onClick={restart}>
              重新获取配对码
            </RemoteButton>
          </div>
        ) : null}
      </section>
    </div>
  );
}
