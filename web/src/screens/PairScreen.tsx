/**
 * Pair screen (W-102, design §6.8): the six-digit code at TV size plus the
 * exact command the operator runs on the Mac mini. No text input is ever
 * required on the TV.
 */

import type { ReactElement } from 'react';

import { useApp } from '../app/context';
import { usePairing } from './usePairing';

function CodeDigits(props: { code: string }): ReactElement {
  return (
    <div className="paircode" aria-label={`Pairing code ${props.code.split('').join(' ')}`}>
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

  return (
    <div className="screen screen--pair">
      <h1 className="pair__title">Pair this screen</h1>

      {pairing.phase === 'starting' || pairing.phase === 'idle' ? (
        <p className="pair__status">Requesting a code…</p>
      ) : null}

      {pairing.code && (pairing.phase === 'waiting' || pairing.phase === 'approved') ? (
        <CodeDigits code={pairing.code} />
      ) : null}

      {pairing.phase === 'waiting' ? (
        <ol className="pair__steps">
          <li>On the Mac mini, run:</li>
          <li>
            <code className="pair__command">
              atrium admin pair approve {pairing.code} --name &quot;Living room TV&quot;
            </code>
          </li>
          <li>This screen continues on its own. The code expires in five minutes.</li>
        </ol>
      ) : null}

      {pairing.phase === 'approved' || pairing.phase === 'claiming' ? (
        <p className="pair__status">Approved — collecting credentials…</p>
      ) : null}

      {pairing.phase === 'claimed' ? (
        <p className="pair__status">Paired as {pairing.screenName}. Loading the dashboard…</p>
      ) : null}

      {pairing.phase === 'expired' ? (
        <p className="pair__status pair__status--warn" role="status">
          <span aria-hidden="true">⚠</span> The code expired. Requesting a new one…
        </p>
      ) : null}

      {pairing.phase === 'error' ? (
        <div className="pair__error" role="status">
          <p>
            <span aria-hidden="true">⚠</span> {pairing.errorMessage ?? 'Pairing failed'}
          </p>
          <button type="button" className="button" onClick={restart}>
            Try again
          </button>
        </div>
      ) : null}
    </div>
  );
}
