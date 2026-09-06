/**
 * Core / NAS / index / connection status (FR-01, FR-07, FR-12, W-204).
 *
 * Every state carries an icon glyph *and* a word: colour is never the only
 * signal, and the text must be readable from 2–3 m, so nothing here goes below
 * 32 px at 1080p. Share free space appears only when the server reports it.
 */

import type { ReactElement } from 'react';

import type { ConnectionState } from '../app/connection';
import type { NasWidgetPayload, PhotoWidgetPayload, SourceHealth } from '../types/api';
import { indexProgressText, nasStatusText, shareFreeText } from './statusText';

type Tone = 'ok' | 'warn' | 'bad' | 'idle';

const HEALTH_TONE: Readonly<Record<SourceHealth, Tone>> = {
  online: 'ok',
  degraded: 'warn',
  offline: 'bad',
  unknown: 'idle',
};

const TONE_GLYPH: Readonly<Record<Tone, string>> = {
  ok: '●',
  warn: '▲',
  bad: '■',
  idle: '○',
};

function StatusChip(props: {
  label: string;
  value: string;
  tone: Tone;
  detail?: string | undefined;
}): ReactElement {
  return (
    <div className={`chip chip--${props.tone}`}>
      <span className="chip__glyph" aria-hidden="true">
        {TONE_GLYPH[props.tone]}
      </span>
      <span className="chip__label">{props.label}</span>
      <span className="chip__value">{props.value}</span>
      {props.detail ? <span className="chip__detail">{props.detail}</span> : null}
    </div>
  );
}

function connectionChip(state: ConnectionState): { value: string; tone: Tone } {
  if (state.stopReason === 'superseded') return { value: 'superseded', tone: 'warn' };
  switch (state.status) {
    case 'online':
      return { value: 'online', tone: 'ok' };
    case 'connecting':
      return { value: 'connecting', tone: 'idle' };
    case 'reconnecting':
      return { value: 'reconnecting', tone: 'warn' };
    case 'offline':
      return { value: 'offline', tone: 'bad' };
    default:
      return { value: 'idle', tone: 'idle' };
  }
}

export interface StatusBarProps {
  connection: ConnectionState;
  nas: NasWidgetPayload | null;
  photo: PhotoWidgetPayload | null;
  homeName: string;
  clientVersion: string;
  nowMs: number;
}

export function StatusBar(props: StatusBarProps): ReactElement {
  const connection = connectionChip(props.connection);
  const sources = props.nas?.sources ?? [];
  const nas = nasStatusText(sources, props.nowMs);
  const indexing = indexProgressText(props.photo);
  const free = shareFreeText(sources);

  return (
    <footer className="statusbar" aria-label="System status">
      <StatusChip
        label="Core"
        value={props.connection.hasSnapshot ? 'reachable' : 'unreachable'}
        tone={props.connection.hasSnapshot ? 'ok' : 'bad'}
      />
      <StatusChip
        label="NAS"
        value={nas.value}
        tone={HEALTH_TONE[nas.health]}
        detail={nas.detail}
      />
      {indexing ? <StatusChip label="Index" value={indexing} tone="warn" /> : null}
      {free ? <StatusChip label="Share" value={free} tone="idle" /> : null}
      <StatusChip label="Link" value={connection.value} tone={connection.tone} />
      <div className="statusbar__spacer" />
      <div className="statusbar__home">{props.homeName}</div>
      <div className="statusbar__version">{props.clientVersion}</div>
    </footer>
  );
}
