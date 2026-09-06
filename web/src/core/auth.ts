/**
 * The single seam for screen-credential transport (design D5 / §7.4).
 *
 * Default transport is the HttpOnly `atrium_screen` cookie set by the pairing
 * claim: fetches use `credentials: 'same-origin'`, `<img>` loads carry the
 * cookie automatically, and the browser WebSocket handshake sends it too.
 *
 * If G0 finds a TV engine that drops cookies, `bearer` mode can be switched on
 * here without touching any screen: it stores the claimed token, sends it as an
 * `Authorization` header, and reports `mediaLoad: 'fetch'` so the image layer
 * switches to fetch + object URLs. A token is never put in a URL.
 */

import { readString, removeKey, writeString } from './storage';

export type AuthMode = 'cookie' | 'bearer';

/** How images must be loaded under the active mode. */
export type MediaLoadStrategy = 'direct' | 'fetch';

const TOKEN_KEY = 'screen_token';

export interface AuthTransport {
  readonly mode: AuthMode;
  readonly mediaLoad: MediaLoadStrategy;
  /** Decorate a fetch init with the credential for this mode. */
  decorate(init?: RequestInit): RequestInit;
  /** Called with the claim response so bearer mode can persist the token. */
  onClaimed(token: string | undefined): void;
  /** Drop every stored credential (revoked, expired, or unpaired). */
  clear(): void;
  /** True when the client believes it holds a credential worth trying. */
  hasCredential(): boolean;
}

class CookieTransport implements AuthTransport {
  readonly mode = 'cookie' as const;
  readonly mediaLoad = 'direct' as const;

  decorate(init: RequestInit = {}): RequestInit {
    return { ...init, credentials: 'same-origin' };
  }

  onClaimed(): void {
    // The server set the cookie; nothing to persist client side.
  }

  clear(): void {
    // An HttpOnly cookie cannot be cleared from script. The server clears it on
    // revoke; until then a 401 routes the client back to pairing.
    removeKey(TOKEN_KEY);
  }

  hasCredential(): boolean {
    // Unknowable for an HttpOnly cookie: assume yes and let the server answer.
    return true;
  }
}

class BearerTransport implements AuthTransport {
  readonly mode = 'bearer' as const;
  readonly mediaLoad = 'fetch' as const;

  decorate(init: RequestInit = {}): RequestInit {
    const token = readString(TOKEN_KEY);
    if (!token) return { ...init, credentials: 'omit' };
    const headers = new Headers(init.headers);
    headers.set('Authorization', `Bearer ${token}`);
    return { ...init, headers, credentials: 'omit' };
  }

  onClaimed(token: string | undefined): void {
    if (token) writeString(TOKEN_KEY, token);
  }

  clear(): void {
    removeKey(TOKEN_KEY);
  }

  hasCredential(): boolean {
    return readString(TOKEN_KEY) !== null;
  }
}

export function createAuthTransport(mode: AuthMode = 'cookie'): AuthTransport {
  return mode === 'bearer' ? new BearerTransport() : new CookieTransport();
}

/** Process-wide transport. Cookie by default (design D5). */
export const authTransport: AuthTransport = createAuthTransport('cookie');
