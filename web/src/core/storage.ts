/**
 * localStorage access. Every call is wrapped: a TV browser in private mode,
 * with storage disabled, or over quota must never break the app.
 */

const PREFIX = 'atrium.';

export function readString(key: string): string | null {
  try {
    return window.localStorage.getItem(PREFIX + key);
  } catch {
    return null;
  }
}

export function writeString(key: string, value: string): void {
  try {
    window.localStorage.setItem(PREFIX + key, value);
  } catch {
    /* storage unavailable — the app must keep working without it */
  }
}

export function removeKey(key: string): void {
  try {
    window.localStorage.removeItem(PREFIX + key);
  } catch {
    /* ignore */
  }
}

export function readNumber(key: string): number | null {
  const raw = readString(key);
  if (raw === null) return null;
  const parsed = Number(raw);
  return Number.isFinite(parsed) ? parsed : null;
}

export function writeNumber(key: string, value: number): void {
  writeString(key, String(value));
}
