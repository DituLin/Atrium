/**
 * HTTP client for `/api/v1` (design §8). All auth transport goes through
 * `AuthTransport`; no credential ever appears in a URL.
 */

import type {
  ApiErrorBody,
  HomeResponse,
  NasStatusResponse,
  PairClaimResponse,
  PairStartResponse,
  PairStatusResponse,
  PhotoCollection,
  PhotoDetailResponse,
  PhotoListResponse,
  ScreenSelfResponse,
} from '../types/api';
import type { AuthTransport } from './auth';
import { authTransport as defaultTransport } from './auth';

export const API_BASE = '/api/v1';

export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  readonly retryAfterMs: number | null;
  readonly details: Record<string, unknown> | undefined;

  constructor(
    status: number,
    code: string,
    message: string,
    retryAfterMs: number | null = null,
    details?: Record<string, unknown>,
  ) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.code = code;
    this.retryAfterMs = retryAfterMs;
    this.details = details;
  }

  get isUnauthorized(): boolean {
    return this.status === 401;
  }

  get isGone(): boolean {
    return this.status === 404 || this.status === 410;
  }
}

export type FetchLike = (input: string, init?: RequestInit) => Promise<Response>;

export interface ApiClientOptions {
  fetchImpl?: FetchLike;
  transport?: AuthTransport;
  /** Called after any successful authenticated response (feeds the 24 h rule). */
  onAuthOk?: () => void;
  /** Called on 401 so the app can route to `pair`. */
  onUnauthorized?: () => void;
}

function parseRetryAfter(header: string | null): number | null {
  if (!header) return null;
  const seconds = Number(header);
  if (Number.isFinite(seconds)) return Math.max(0, seconds * 1000);
  const date = Date.parse(header);
  return Number.isNaN(date) ? null : Math.max(0, date - Date.now());
}

export class ApiClient {
  private readonly fetchImpl: FetchLike;
  private readonly transport: AuthTransport;
  private readonly onAuthOk: (() => void) | undefined;
  private readonly onUnauthorized: (() => void) | undefined;

  constructor(options: ApiClientOptions = {}) {
    this.fetchImpl =
      options.fetchImpl ?? ((input, init) => globalThis.fetch(input, init));
    this.transport = options.transport ?? defaultTransport;
    this.onAuthOk = options.onAuthOk;
    this.onUnauthorized = options.onUnauthorized;
  }

  /** Raw request used by both authenticated and public routes. */
  private async request<T>(
    path: string,
    init: RequestInit,
    authenticated: boolean,
  ): Promise<T> {
    const finalInit = authenticated ? this.transport.decorate(init) : { ...init };
    const response = await this.fetchImpl(path, finalInit);

    if (response.status === 401) {
      this.onUnauthorized?.();
      throw await toApiError(response, 'unauthorized');
    }
    if (!response.ok) {
      throw await toApiError(response, 'internal');
    }
    if (authenticated) this.onAuthOk?.();
    if (response.status === 204) return undefined as T;
    return (await response.json()) as T;
  }

  private get<T>(path: string, authenticated = true): Promise<T> {
    return this.request<T>(path, { method: 'GET', headers: { Accept: 'application/json' } }, authenticated);
  }

  private post<T>(path: string, body: unknown, authenticated = true): Promise<T> {
    return this.request<T>(
      path,
      {
        method: 'POST',
        headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
        body: JSON.stringify(body ?? {}),
      },
      authenticated,
    );
  }

  /* ---------------------------------------------------------- pairing */

  pairStart(clientHint: string): Promise<PairStartResponse> {
    return this.post<PairStartResponse>(`${API_BASE}/pair/start`, { client_hint: clientHint }, false);
  }

  pairStatus(pairingId: string): Promise<PairStatusResponse> {
    return this.get<PairStatusResponse>(`${API_BASE}/pair/${encodeURIComponent(pairingId)}`, false);
  }

  async pairClaim(pairingId: string): Promise<PairClaimResponse> {
    const claimed = await this.request<PairClaimResponse>(
      `${API_BASE}/pair/${encodeURIComponent(pairingId)}/claim`,
      {
        method: 'POST',
        headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
        body: '{}',
        credentials: 'same-origin',
      },
      false,
    );
    this.transport.onClaimed(claimed.token);
    this.onAuthOk?.();
    return claimed;
  }

  /* ------------------------------------------------------------ data */

  getHome(): Promise<HomeResponse> {
    return this.get<HomeResponse>(`${API_BASE}/home`);
  }

  getNasStatus(): Promise<NasStatusResponse> {
    return this.get<NasStatusResponse>(`${API_BASE}/nas/status`);
  }

  getScreenSelf(): Promise<ScreenSelfResponse> {
    return this.get<ScreenSelfResponse>(`${API_BASE}/screens/me`);
  }

  listPhotos(params: {
    collection: PhotoCollection;
    cursor?: string | null;
    limit?: number;
    seed?: string | null;
  }): Promise<PhotoListResponse> {
    const query = new URLSearchParams({ collection: params.collection });
    if (params.cursor) query.set('cursor', params.cursor);
    if (params.limit) query.set('limit', String(params.limit));
    if (params.seed) query.set('seed', params.seed);
    return this.get<PhotoListResponse>(`${API_BASE}/photos?${query.toString()}`);
  }

  getPhoto(id: string, neighbors?: PhotoCollection): Promise<PhotoDetailResponse> {
    const query = neighbors ? `?neighbors=${encodeURIComponent(neighbors)}` : '';
    return this.get<PhotoDetailResponse>(`${API_BASE}/photos/${encodeURIComponent(id)}${query}`);
  }

  /** Media URL for `<img src>`; ID-based only, never carries a credential. */
  mediaUrl(id: string, variant: 'preview' | 'thumb' = 'preview'): string {
    return `${API_BASE}/media/photos/${encodeURIComponent(id)}?variant=${variant}`;
  }

  /**
   * HEAD-like probe used by the slideshow and the viewer to classify
   * `202 / 404 / 410 / 503` before committing to an `<img>` element.
   *
   * It is a ranged GET (`Range: bytes=0-0`), so a server that honours ranges —
   * Go's `http.ServeContent` does — answers `206 Partial Content` rather than
   * `200`. Both mean "the bytes are there": `classifyMediaStatus` maps them to
   * `ready`, and both count as a successful authenticated response for the 24 h
   * auth-cache rule (PRD 8.2).
   */
  async probeMedia(id: string, variant: 'preview' | 'thumb' = 'preview'): Promise<number> {
    const response = await this.fetchImpl(
      this.mediaUrl(id, variant),
      this.transport.decorate({ method: 'GET', headers: { Range: 'bytes=0-0' } }),
    );
    if (response.status === 401) this.onUnauthorized?.();
    else if (isMediaSuccess(response.status)) this.onAuthOk?.();
    return response.status;
  }
}

/** `200 OK` and `206 Partial Content` are both "the preview is there". */
export function isMediaSuccess(status: number): boolean {
  return status === 200 || status === 206 || status === 304;
}

async function toApiError(response: Response, fallbackCode: string): Promise<ApiError> {
  let code = fallbackCode;
  let message = response.statusText || fallbackCode;
  let details: Record<string, unknown> | undefined;
  try {
    const body = (await response.json()) as Partial<ApiErrorBody>;
    if (body && body.error) {
      code = body.error.code || code;
      message = body.error.message || message;
      details = body.error.details;
    }
  } catch {
    /* non-JSON error bodies are acceptable */
  }
  return new ApiError(
    response.status,
    code,
    message,
    parseRetryAfter(response.headers.get('Retry-After')),
    details,
  );
}
