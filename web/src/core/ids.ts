/**
 * Client-side message IDs for the WS envelope (`msg_<random>`).
 * `crypto.randomUUID` is not available on every TV engine, so this falls back
 * to `crypto.getRandomValues` and finally to `Math.random`.
 */

const ALPHABET = '0123456789abcdefghijklmnopqrstuvwxyz';

function randomChars(length: number): string {
  const cryptoObj: Crypto | undefined =
    typeof globalThis.crypto !== 'undefined' ? globalThis.crypto : undefined;
  let out = '';
  if (cryptoObj && typeof cryptoObj.getRandomValues === 'function') {
    const bytes = new Uint8Array(length);
    cryptoObj.getRandomValues(bytes);
    for (let i = 0; i < length; i += 1) {
      out += ALPHABET[(bytes[i] ?? 0) % ALPHABET.length];
    }
    return out;
  }
  for (let i = 0; i < length; i += 1) {
    out += ALPHABET[Math.floor(Math.random() * ALPHABET.length)];
  }
  return out;
}

export function newMessageId(): string {
  return `msg_${Date.now().toString(36)}${randomChars(10)}`;
}

/** Seed for a `random` collection round (design §6.4). */
export function newRandomSeed(): string {
  return randomChars(12);
}
