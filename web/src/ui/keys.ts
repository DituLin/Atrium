/**
 * Remote-control key mapping (design §7.1).
 *
 * TV engines disagree about `KeyboardEvent.key`, so the numeric `keyCode` is
 * still the only reliable signal for the Back button:
 *   461   webOS (LG)
 *   10009 Tizen (Samsung)
 * Android TV WebViews send `KeyEvent.KEYCODE_BACK` as `Backspace`, `GoBack` or
 * `BrowserBack` depending on the wrapper, so all three are accepted.
 */

export type RemoteKey = 'left' | 'right' | 'up' | 'down' | 'enter' | 'back';

/** webOS "Back". */
export const KEYCODE_WEBOS_BACK = 461;
/** Tizen "Return". */
export const KEYCODE_TIZEN_RETURN = 10009;

export interface KeyLikeEvent {
  key?: string | undefined;
  keyCode?: number | undefined;
}

const BY_KEY: Readonly<Record<string, RemoteKey>> = {
  ArrowLeft: 'left',
  Left: 'left',
  ArrowRight: 'right',
  Right: 'right',
  ArrowUp: 'up',
  Up: 'up',
  ArrowDown: 'down',
  Down: 'down',
  Enter: 'enter',
  ' ': 'enter',
  Spacebar: 'enter',
  Select: 'enter',
  OK: 'enter',
  Escape: 'back',
  Esc: 'back',
  Backspace: 'back',
  GoBack: 'back',
  BrowserBack: 'back',
  XF86Back: 'back',
};

const BY_KEYCODE: Readonly<Record<number, RemoteKey>> = {
  37: 'left',
  38: 'up',
  39: 'right',
  40: 'down',
  13: 'enter',
  27: 'back',
  8: 'back',
  [KEYCODE_WEBOS_BACK]: 'back',
  [KEYCODE_TIZEN_RETURN]: 'back',
};

/** `null` for keys the app does not handle (they must stay with the browser). */
export function mapRemoteKey(event: KeyLikeEvent): RemoteKey | null {
  if (event.key !== undefined) {
    const byKey = BY_KEY[event.key];
    if (byKey) return byKey;
  }
  if (typeof event.keyCode === 'number') {
    const byCode = BY_KEYCODE[event.keyCode];
    if (byCode) return byCode;
  }
  return null;
}

export function isDirection(key: RemoteKey): key is 'left' | 'right' | 'up' | 'down' {
  return key === 'left' || key === 'right' || key === 'up' || key === 'down';
}
