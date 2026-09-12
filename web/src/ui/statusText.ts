/**
 * Status text (W-204, FR-07/FR-12). Pure string building so the wording can be
 * asserted in a test instead of eyeballed on a TV.
 *
 * Two product rules are encoded here: every state carries a word (colour is
 * never the only signal), and free space is only ever presented as the *share's*
 * free space — never as NAS capacity or RAID health — and disappears entirely
 * when the server does not report it.
 */

import type { NasSourceStatus, PhotoWidgetPayload, SourceHealth } from '../types/api';

export const HEALTH_ORDER: readonly SourceHealth[] = ['offline', 'degraded', 'unknown', 'online'];

export function worstHealth(sources: readonly NasSourceStatus[]): SourceHealth {
  if (sources.length === 0) return 'unknown';
  for (const health of HEALTH_ORDER) {
    if (sources.some((source) => source.health === health)) return health;
  }
  return 'unknown';
}

/** Relative elapsed time; absent or invalid timestamps remain explicitly unknown. */
export function relativeTime(iso: string | null | undefined, nowMs: number): string {
  if (!iso) return '时间未知';
  const epoch = Date.parse(iso);
  if (Number.isNaN(epoch)) return '时间未知';
  const seconds = Math.max(0, Math.round((nowMs - epoch) / 1000));
  if (seconds < 60) return '刚刚';
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes} 分钟前`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours} 小时前`;
  return `${Math.floor(hours / 24)} 天前`;
}

const UNITS = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'] as const;

export function formatBytes(bytes: number): string {
  let value = Math.max(0, bytes);
  let unit = 0;
  while (value >= 1024 && unit < UNITS.length - 1) {
    value /= 1024;
    unit += 1;
  }
  const rounded = value >= 100 || unit === 0 ? Math.round(value) : Math.round(value * 10) / 10;
  return `${rounded} ${UNITS[unit]}`;
}

/** FR-12: only shown when the server reports it, and only as share free space. */
export function shareFreeText(sources: readonly NasSourceStatus[]): string | null {
  for (const source of sources) {
    const free = source.share_free_bytes;
    if (typeof free === 'number' && Number.isFinite(free)) {
      return `剩余 ${formatBytes(free)}`;
    }
  }
  return null;
}

const HEALTH_TEXT: Readonly<Record<SourceHealth, string>> = {
  online: '在线',
  degraded: '异常',
  offline: '离线',
  unknown: '未知',
};

export interface NasStatusText {
  health: SourceHealth;
  /** The word shown next to the glyph. */
  value: string;
  /** Secondary line: which source, and when it was last checked. */
  detail: string;
}

export function nasStatusText(
  sources: readonly NasSourceStatus[],
  nowMs: number,
): NasStatusText {
  if (sources.length === 0) {
    return { health: 'unknown', value: '未知', detail: '未配置照片来源' };
  }
  const health = worstHealth(sources);
  const affected = sources.find((source) => source.health === health) ?? sources[0];
  const checked = relativeTime(affected?.last_check_at, nowMs);
  const name = affected?.name ?? '照片来源';
  const detail = checked === '时间未知' ? '检查时间未知' : `上次检查：${checked}`;
  return {
    health,
    value: HEALTH_TEXT[health],
    detail: sources.length === 1 ? detail : `${name} · ${detail}`,
  };
}

/** Baseline import / scan progress for the status bar, or null when idle. */
export function indexProgressText(payload: PhotoWidgetPayload | null): string | null {
  if (!payload) return null;
  const { index, baseline } = payload;
  const { seen, indexed } = index.progress;
  if (index.state === 'baseline_import' || baseline.status === 'importing') {
    const percent = seen > 0 ? Math.min(100, Math.floor((indexed / seen) * 100)) : 0;
    return `首次导入 ${percent}%（${indexed} / ${seen}）`;
  }
  if (index.state === 'scanning') return `扫描中 · 已索引 ${indexed} 项`;
  return null;
}
