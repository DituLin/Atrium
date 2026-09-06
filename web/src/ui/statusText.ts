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

/** "just now" / "4 min ago" / "3 h ago" / "2 d ago"; "never" without a time. */
export function relativeTime(iso: string | null | undefined, nowMs: number): string {
  if (!iso) return 'never';
  const epoch = Date.parse(iso);
  if (Number.isNaN(epoch)) return 'never';
  const seconds = Math.max(0, Math.round((nowMs - epoch) / 1000));
  if (seconds < 60) return 'just now';
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes} min ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours} h ago`;
  return `${Math.floor(hours / 24)} d ago`;
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
      return `${formatBytes(free)} free`;
    }
  }
  return null;
}

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
    return { health: 'unknown', value: 'unknown', detail: 'no source configured' };
  }
  const health = worstHealth(sources);
  const affected = sources.find((source) => source.health === health) ?? sources[0];
  const checked = relativeTime(affected?.last_check_at, nowMs);
  const name = affected?.name ?? 'source';
  return {
    health,
    value: health,
    detail: sources.length === 1 ? `checked ${checked}` : `${name} · checked ${checked}`,
  };
}

/** Baseline import / scan progress for the status bar, or null when idle. */
export function indexProgressText(payload: PhotoWidgetPayload | null): string | null {
  if (!payload) return null;
  const { index, baseline } = payload;
  const { seen, indexed } = index.progress;
  if (index.state === 'baseline_import' || baseline.status === 'importing') {
    const percent = seen > 0 ? Math.min(100, Math.floor((indexed / seen) * 100)) : 0;
    return `first import ${percent}% (${indexed} of ${seen})`;
  }
  if (index.state === 'scanning') return `scanning · ${indexed} indexed`;
  return null;
}
