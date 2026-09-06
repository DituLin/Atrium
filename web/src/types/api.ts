/**
 * HTTP contract types — hand-mirrored from tech-design §8 and §6.7.
 * Keep in sync with docs/api/openapi.yaml; the design document is the authority.
 */

export const PHOTO_COLLECTIONS = ['recent', 'captured_today', 'random', 'all'] as const;
export type PhotoCollection = (typeof PHOTO_COLLECTIONS)[number];

export function isPhotoCollection(value: unknown): value is PhotoCollection {
  return typeof value === 'string' && (PHOTO_COLLECTIONS as readonly string[]).includes(value);
}

export type PreviewStatus =
  | 'pending'
  | 'processing'
  | 'ready'
  | 'failed'
  | 'evicted'
  | 'unavailable';

export type CapturedConfidence = 'exact' | 'inferred' | 'unknown';

export type SourceHealth = 'online' | 'offline' | 'degraded' | 'unknown';

export interface PhotoPreview {
  status: PreviewStatus;
  width: number;
  height: number;
}

export interface PhotoUrls {
  preview: string;
  thumb: string;
}

export interface PhotoItem {
  id: string;
  source_id: string;
  captured_at: string | null;
  captured_confidence: CapturedConfidence;
  first_seen_at: string;
  is_baseline: boolean;
  width: number;
  height: number;
  preview: PhotoPreview;
  urls: PhotoUrls;
}

export interface PhotoListMeta {
  /** Only present for `captured_today`. */
  unknown_captured_count?: number;
  /** Only present for `random`; echoes the seed the round was built with. */
  seed?: string;
  total?: number;
}

export interface PhotoListResponse {
  items: PhotoItem[];
  next_cursor: string | null;
  meta?: PhotoListMeta;
}

export interface PhotoNeighbors {
  previous_id: string | null;
  next_id: string | null;
}

export interface PhotoDetailResponse {
  item: PhotoItem;
  neighbors?: PhotoNeighbors;
}

export interface NasSourceStatus {
  id: string;
  name: string;
  health: SourceHealth;
  last_check_at: string | null;
  last_success_at: string | null;
  share_free_bytes?: number | null;
  share_total_bytes?: number | null;
}

export interface NasStatusResponse {
  sources: NasSourceStatus[];
}

/* ---------------------------------------------------------------- widgets */

export interface ClockWidgetPayload {
  timezone: string;
  server_time: string;
  utc_offset_seconds: number;
  next_offset_change_at?: string | null;
}

export type IndexState = 'idle' | 'scanning' | 'baseline_import';
export type BaselineStatus = 'importing' | 'done' | 'none';

export interface PhotoWidgetPayload {
  slideshow_interval_seconds: number;
  totals: { ready: number; pending_preview: number; unsupported: number };
  new_today: number;
  captured_today: number;
  unknown_captured: number;
  baseline: { status: BaselineStatus; completed_at: string | null };
  index: {
    state: IndexState;
    progress: { seen: number; indexed: number; pending_preview: number };
    last_scan_at: string | null;
  };
}

export interface NasWidgetPayload {
  sources: NasSourceStatus[];
}

export interface WeatherWidgetPayload {
  provider: string;
  location_label: string;
  temperature_c: number | null;
  condition_code: string | null;
  condition_text: string | null;
  fetched_at: string | null;
  stale: boolean;
}

export interface NoticeWidgetPayload {
  text: string;
  updated_at: string | null;
}

export const KNOWN_WIDGET_TYPES = ['clock', 'photo', 'nas', 'weather', 'notice'] as const;
export type KnownWidgetType = (typeof KNOWN_WIDGET_TYPES)[number];

interface WidgetEnvelope<T extends string, P> {
  type: T;
  payload: P;
}

export type ClockWidget = WidgetEnvelope<'clock', ClockWidgetPayload>;
export type PhotoWidget = WidgetEnvelope<'photo', PhotoWidgetPayload>;
export type NasWidget = WidgetEnvelope<'nas', NasWidgetPayload>;
export type WeatherWidget = WidgetEnvelope<'weather', WeatherWidgetPayload>;
export type NoticeWidget = WidgetEnvelope<'notice', NoticeWidgetPayload>;

/** Anything the server may add later. The client hides types it does not know. */
export interface UnknownWidget {
  type: string;
  payload: unknown;
}

export type Widget =
  | ClockWidget
  | PhotoWidget
  | NasWidget
  | WeatherWidget
  | NoticeWidget
  | UnknownWidget;

export interface HomeResponse {
  schema_version: 1;
  server_time: string;
  version: number;
  home: { name: string; timezone: string };
  widgets: Widget[];
}

/* ---------------------------------------------------------------- pairing */

export type PairingStatus = 'pending' | 'approved' | 'claimed' | 'expired' | 'rejected';

export interface PairStartResponse {
  pairing_id: string;
  code: string;
  expires_at: string;
  poll_interval_ms: number;
}

export interface PairStatusResponse {
  pairing_id: string;
  status: PairingStatus;
  expires_at: string;
}

export interface PairClaimResponse {
  screen_id: string;
  name: string;
  /** Only used by non-browser clients / the §7.4 bearer fallback. */
  token?: string;
}

export interface ScreenSelfResponse {
  id: string;
  name: string;
  status: 'active' | 'revoked';
}

/* ----------------------------------------------------------------- errors */

export const API_ERROR_CODES = [
  'unauthorized',
  'forbidden',
  'not_found',
  'invalid_request',
  'invalid_command',
  'screen_offline',
  'screen_revoked',
  'pairing_expired',
  'pairing_claimed',
  'rate_limited',
  'preview_processing',
  'preview_unavailable',
  'source_offline',
  'identity_mismatch',
  'conflict',
  'internal',
] as const;
export type ApiErrorCode = (typeof API_ERROR_CODES)[number];

export interface ApiErrorBody {
  error: {
    code: string;
    message: string;
    details?: Record<string, unknown>;
  };
}
