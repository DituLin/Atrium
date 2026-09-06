/**
 * Mock core state: pairings, screens, tokens and the home snapshot.
 * Development only — never bundled, never imported from `src/`.
 */

import { randomBytes } from 'node:crypto';

import { HOME_NAME, HOME_TIMEZONE, HOME_UTC_OFFSET_SECONDS, PHOTOS } from './data.ts';

export interface Pairing {
  pairing_id: string;
  code: string;
  status: 'pending' | 'approved' | 'claimed' | 'expired' | 'rejected';
  expires_at: string;
  screen_id: string;
  name: string;
}

export interface Screen {
  id: string;
  name: string;
  token: string;
}

export const PAIR_TTL_MS = 5 * 60 * 1000;
export const POLL_INTERVAL_MS = 3000;

export const pairings = new Map<string, Pairing>();
export const screens = new Map<string, Screen>();
/** token → screen id */
export const tokens = new Map<string, string>();

let counter = 0;

export function startPairing(): Pairing {
  counter += 1;
  const pairing: Pairing = {
    pairing_id: `pr_${counter}_${randomBytes(4).toString('hex')}`,
    code: String(100000 + Math.floor(Math.random() * 900000)),
    status: 'pending',
    expires_at: new Date(Date.now() + PAIR_TTL_MS).toISOString(),
    screen_id: `screen_${counter}`,
    name: `Mock screen ${counter}`,
  };
  pairings.set(pairing.pairing_id, pairing);
  return pairing;
}

export function findByCode(code: string): Pairing | undefined {
  for (const pairing of pairings.values()) {
    if (pairing.code === code) return pairing;
  }
  return undefined;
}

export function approve(pairing: Pairing, name?: string, screenId?: string): void {
  pairing.status = 'approved';
  if (name) pairing.name = name;
  if (screenId) pairing.screen_id = screenId;
}

export function claim(pairing: Pairing): Screen {
  const token = `atr_scr_${randomBytes(32).toString('base64url')}`;
  const screen: Screen = { id: pairing.screen_id, name: pairing.name, token };
  screens.set(screen.id, screen);
  tokens.set(token, screen.id);
  pairing.status = 'claimed';
  return screen;
}

export function screenForToken(token: string | null): Screen | null {
  if (!token) return null;
  const id = tokens.get(token);
  return id ? (screens.get(id) ?? null) : null;
}

/**
 * Overridable slices of the home snapshot, driven by `/__control/state`, so the
 * status bar's index progress and NAS health (W-204) can be exercised without
 * a real indexer:
 *
 *   POST /__control/state {"index": {"state": "baseline_import", "seen": 200,
 *                                    "indexed": 40}, "nas_health": "degraded"}
 */
export interface StateOverrides {
  index_state: 'idle' | 'scanning' | 'baseline_import';
  seen: number;
  indexed: number;
  baseline: 'importing' | 'done' | 'none';
  nas_health: 'online' | 'offline' | 'degraded' | 'unknown';
  share_free_bytes: number | null;
  slideshow_interval_seconds: number;
}

export const overrides: StateOverrides = {
  index_state: 'idle',
  seen: PHOTOS.length,
  indexed: PHOTOS.length,
  baseline: 'done',
  nas_health: 'online',
  share_free_bytes: 512 * 1024 ** 3,
  slideshow_interval_seconds: 30,
};

export function applyOverrides(patch: Record<string, unknown>): StateOverrides {
  for (const key of Object.keys(overrides) as (keyof StateOverrides)[]) {
    if (patch[key] !== undefined) {
      (overrides as unknown as Record<string, unknown>)[key] = patch[key];
    }
  }
  return overrides;
}

let homeVersion = 1;

export function bumpHomeVersion(): number {
  homeVersion += 1;
  return homeVersion;
}

export function homeSnapshot(): unknown {
  const now = new Date();
  return {
    schema_version: 1,
    server_time: now.toISOString(),
    version: homeVersion,
    home: { name: HOME_NAME, timezone: HOME_TIMEZONE },
    widgets: [
      {
        type: 'clock',
        payload: {
          timezone: HOME_TIMEZONE,
          server_time: now.toISOString(),
          utc_offset_seconds: HOME_UTC_OFFSET_SECONDS,
          next_offset_change_at: null,
        },
      },
      {
        type: 'photo',
        payload: {
          slideshow_interval_seconds: overrides.slideshow_interval_seconds,
          totals: {
            ready: PHOTOS.filter((p) => p.preview_status === 'ready').length,
            pending_preview: PHOTOS.filter((p) => p.preview_status === 'pending').length,
            unsupported: PHOTOS.filter((p) => p.preview_status === 'failed').length,
          },
          new_today: PHOTOS.filter((p) => !p.is_baseline).length,
          captured_today: PHOTOS.filter((p) => p.captured_at !== null).length,
          unknown_captured: PHOTOS.filter((p) => p.captured_at === null).length,
          baseline: { status: overrides.baseline, completed_at: now.toISOString() },
          index: {
            state: overrides.index_state,
            progress: {
              seen: overrides.seen,
              indexed: overrides.indexed,
              pending_preview: PHOTOS.filter((p) => p.preview_status === 'pending').length,
            },
            last_scan_at: now.toISOString(),
          },
        },
      },
      { type: 'nas', payload: nasStatus() },
      // An unknown type on purpose: the client must hide it silently.
      { type: 'experimental_ticker', payload: { text: 'should not render' } },
    ],
  };
}

export function nasStatus(): { sources: unknown[] } {
  return {
    sources: [
      {
        id: 'family_photos',
        name: 'Family photos',
        health: overrides.nas_health,
        last_check_at: new Date().toISOString(),
        last_success_at: new Date().toISOString(),
        share_free_bytes: overrides.share_free_bytes,
        share_total_bytes: 4 * 1024 ** 4,
      },
    ],
  };
}
