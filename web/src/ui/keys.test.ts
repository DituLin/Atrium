import { describe, expect, it } from 'vitest';

import { KEYCODE_TIZEN_RETURN, KEYCODE_WEBOS_BACK, mapRemoteKey } from './keys';

describe('remote key mapping (design §7.1)', () => {
  const table: Array<[{ key?: string; keyCode?: number }, string | null]> = [
    [{ key: 'ArrowLeft' }, 'left'],
    [{ key: 'ArrowRight' }, 'right'],
    [{ key: 'ArrowUp' }, 'up'],
    [{ key: 'ArrowDown' }, 'down'],
    [{ key: 'Left' }, 'left'],
    [{ key: 'Enter' }, 'enter'],
    [{ key: 'Escape' }, 'back'],
    [{ key: 'Backspace' }, 'back'],
    [{ key: 'GoBack' }, 'back'],
    [{ key: 'BrowserBack' }, 'back'],
    // webOS and Tizen only send the numeric code.
    [{ key: 'Unidentified', keyCode: KEYCODE_WEBOS_BACK }, 'back'],
    [{ key: 'Unidentified', keyCode: KEYCODE_TIZEN_RETURN }, 'back'],
    [{ keyCode: 461 }, 'back'],
    [{ keyCode: 10009 }, 'back'],
    [{ keyCode: 37 }, 'left'],
    [{ key: 'a' }, null],
    [{ key: 'F5' }, null],
    [{ keyCode: 999 }, null],
    [{}, null],
  ];

  for (const [event, expected] of table) {
    it(`maps ${JSON.stringify(event)} to ${String(expected)}`, () => {
      expect(mapRemoteKey(event)).toBe(expected);
    });
  }
});
