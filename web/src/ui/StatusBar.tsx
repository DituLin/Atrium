/**
 * Core / NAS / index / connection status (FR-01, FR-07, FR-12, W-204).
 *
 * Every state carries an icon glyph *and* a word: colour is never the only
 * signal. Typography scales with the effective TV viewport. Share free space
 * appears only when the server reports it.
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
  if (state.stopReason === 'superseded') return { value: '另一会话已接管', tone: 'warn' };
  switch (state.status) {
    case 'online':
      return { value: '已连接', tone: 'ok' };
    case 'connecting':
      return { value: '正在连接', tone: 'idle' };
    case 'reconnecting':
      return { value: '正在重连', tone: 'warn' };
    case 'offline':
      return { value: '已断开', tone: 'bad' };
    default:
      return { value: '未连接', tone: 'idle' };
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
  const online = props.connection.status === 'online';
  const snapshotDetail = props.connection.hasSnapshot ? '显示上次数据' : '尚未取得数据';

  return (
    <footer className="statusbar" aria-label="系统状态">
      <StatusChip
        label="家庭服务"
        value={online ? '已连接' : '连接未确认'}
        tone={online ? 'ok' : 'idle'}
        detail={online ? undefined : snapshotDetail}
      />
      <StatusChip
        label="NAS"
        value={nas.value}
        tone={HEALTH_TONE[nas.health]}
        detail={nas.detail}
      />
      {indexing ? <StatusChip label="索引" value={indexing} tone="warn" /> : null}
      {free ? <StatusChip label="共享空间" value={free} tone="idle" /> : null}
      <StatusChip label="连接" value={connection.value} tone={connection.tone} />
      <div className="statusbar__spacer" />
      <div className="statusbar__home">{props.homeName}</div>
      <div className="statusbar__version">{props.clientVersion}</div>
    </footer>
  );
}
