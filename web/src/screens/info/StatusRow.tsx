import type { ReactElement, ReactNode } from 'react';

import type { Tone } from './familyDate';

/** One hairline row: dot · label · value. Status rows always carry a word, never only a colour. */
export function StatusRow(props: { tone?: Tone; label: ReactNode; value: ReactNode; help?: ReactNode }): ReactElement {
  return (
    <div className="row info-row">
      {props.tone ? <span className={`dot dot--${props.tone}`} aria-hidden="true" /> : null}
      <span className="info-row__label">
        <span>{props.label}</span>
        {props.help ? <span className="info-row__help">{props.help}</span> : null}
      </span>
      <span className={`row__value${props.tone === 'ok' || !props.tone ? '' : ' row__value--muted'}`}>{props.value}</span>
    </div>
  );
}
