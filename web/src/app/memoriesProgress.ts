/** A small opaque bookmark, never photo bytes, URLs, credentials or an album. */
import { readString, removeKey, writeString } from '../core/storage';

const KEY = 'memories_progress';
export interface MemoriesProgress { v: 1; day: string; screenID: string; resumeCursor: string }
export function loadMemoriesProgress(screenID: string, day?: string): MemoriesProgress | null {
  try {
    const raw: unknown = JSON.parse(readString(KEY) ?? 'null');
    if (!raw || typeof raw !== 'object') return null;
    const value = raw as Partial<MemoriesProgress>;
    if (value.v !== 1 || typeof value.day !== 'string' || !/^\d{4}-\d{2}-\d{2}$/.test(value.day)
      || value.screenID !== screenID || (day && value.day !== day)
      || typeof value.resumeCursor !== 'string' || !value.resumeCursor || value.resumeCursor.length > 4096) return null;
    return { v: 1, day: value.day, screenID, resumeCursor: value.resumeCursor };
  } catch { return null; }
}
export function saveMemoriesProgress(value: MemoriesProgress): void {
  writeString(KEY, JSON.stringify({ v: 1, day: value.day, screenID: value.screenID, resumeCursor: value.resumeCursor }));
}
export function clearMemoriesProgress(): void { removeKey(KEY); }
