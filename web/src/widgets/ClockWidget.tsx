/**
 * Clock (FR-02). Time comes from the server offset; the browser only ticks.
 * The date is formatted in the home timezone, so it rolls over at local
 * midnight rather than at the device's midnight. When no server time has
 * arrived for six hours a "time unverified" badge appears.
 */

import type { ReactElement } from 'react';

import type { ClockState } from '../core/clock';
import { formatClock, isTimeUnverified, serverNow, supportsTimeZone } from '../core/clock';
import type { ClockWidgetPayload } from '../types/api';

export interface ClockWidgetProps {
  payload: ClockWidgetPayload;
  clock: ClockState;
  nowMs: number;
}

export function ClockWidget(props: ClockWidgetProps): ReactElement {
  const { payload, clock, nowMs } = props;
  const options = {
    timezone: payload.timezone,
    utcOffsetSeconds: payload.utc_offset_seconds,
    timeZoneSupported: supportsTimeZone(payload.timezone),
  };
  const formatted = formatClock(serverNow(clock, nowMs), options);
  const unverified = isTimeUnverified(clock, nowMs);

  return (
    <section className="widget widget--clock" aria-label="Clock">
      <div className="clock__time">
        <span className="clock__hhmm">{formatted.time}</span>
        <span className="clock__seconds">{formatted.seconds}</span>
      </div>
      <div className="clock__date">{formatted.date}</div>
      <div className="clock__zone">{payload.timezone}</div>
      {unverified ? (
        <div className="badge badge--warn" role="status">
          <span aria-hidden="true">⚠</span> time unverified
        </div>
      ) : null}
    </section>
  );
}
