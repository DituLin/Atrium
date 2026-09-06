/**
 * Optional weather widget (FR-05). The server only emits it when it is enabled
 * and has a payload, so there is never an empty card. Stale readings are marked
 * with text, not colour alone.
 */

import type { ReactElement } from 'react';

import type { WeatherWidgetPayload } from '../types/api';

export function WeatherWidget(props: { payload: WeatherWidgetPayload }): ReactElement {
  const { payload } = props;
  return (
    <section className="widget widget--weather" aria-label="Weather">
      <div className="weather__temp">
        {payload.temperature_c === null ? '—' : `${Math.round(payload.temperature_c)}°`}
      </div>
      <div className="weather__meta">
        <div className="weather__condition">{payload.condition_text ?? 'Unknown'}</div>
        <div className="weather__place">{payload.location_label}</div>
        <div className="weather__source">
          {payload.provider}
          {payload.stale ? ' · stale' : ''}
        </div>
      </div>
    </section>
  );
}
