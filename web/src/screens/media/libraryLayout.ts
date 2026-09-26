/**
 * Pure layout rules for the 影像 library grid: month sections and the remote
 * movement across them. Kept out of React so the rules are testable alone.
 */

import type { ClockFormatOptions } from '../../core/clock';
import { wallClockParts } from '../../core/clock';
import type { PhotoCollection, PhotoItem } from '../../types/api';
import type { RemoteKey } from '../../ui/keys';

export interface GridSection {
  /** Month header, or null for an ungrouped grid. */
  label: string | null;
  /** Index of the section's first item in the flat list. */
  start: number;
  count: number;
}

export const UNKNOWN_MONTH = '拍摄时间未知';

/** Only `all` is ordered by capture time (undated last); others stay flat. */
export function isDateOrdered(collection: PhotoCollection): boolean {
  return collection === 'all';
}

export function monthLabel(item: PhotoItem, options: ClockFormatOptions): string {
  if (item.captured_confidence === 'unknown' || item.captured_at === null) return UNKNOWN_MONTH;
  const epoch = Date.parse(item.captured_at);
  if (Number.isNaN(epoch)) return UNKNOWN_MONTH;
  const parts = wallClockParts(epoch, options);
  return `${parts.year}年${parts.month}月`;
}

/**
 * Consecutive items that share a capture month form one section. A grid with
 * no capture dates at all is not worth headers, so it falls back to one flat
 * section, as does any collection that is not ordered by capture time.
 */
export function gridSections(
  items: readonly PhotoItem[],
  collection: PhotoCollection,
  options: ClockFormatOptions,
): GridSection[] {
  const flat = [{ label: null, start: 0, count: items.length }];
  if (!isDateOrdered(collection) || items.length === 0) return flat;
  const sections: GridSection[] = [];
  items.forEach((item, index) => {
    const label = monthLabel(item, options);
    const last = sections[sections.length - 1];
    if (last && last.label === label) last.count += 1;
    else sections.push({ label, start: index, count: 1 });
  });
  return sections.every(section => section.label === UNKNOWN_MONTH) ? flat : sections;
}

interface Cell { section: number; row: number; column: number }

function locate(sections: readonly GridSection[], index: number, columns: number): Cell | null {
  for (let s = 0; s < sections.length; s++) {
    const section = sections[s]!;
    if (index >= section.start && index < section.start + section.count) {
      const offset = index - section.start;
      return { section: s, row: Math.floor(offset / columns), column: offset % columns };
    }
  }
  return null;
}

function rowCount(section: GridSection, columns: number): number {
  return Math.ceil(section.count / columns);
}

/**
 * Next focus index for a direction, or -1 when the key leaves the grid (Up from
 * the first row, Left from the first column). Other edges stay put; nothing
 * wraps. Inside a section a short last row keeps the focus (as a flat grid
 * does); crossing into another section clamps to that row's last tile.
 */
export function moveInSections(
  sections: readonly GridSection[],
  index: number,
  direction: RemoteKey,
  columns: number,
): number {
  const cols = Math.max(1, columns);
  const cell = locate(sections, index, cols);
  if (!cell) return -1;
  const section = sections[cell.section]!;
  const rowStart = section.start + cell.row * cols;
  const rowEnd = Math.min(section.start + section.count, rowStart + cols) - 1;
  switch (direction) {
    case 'left':
      return cell.column === 0 ? -1 : index - 1;
    case 'right':
      return index >= rowEnd ? index : index + 1;
    case 'up': {
      if (cell.row > 0) return index - cols;
      if (cell.section === 0) return -1;
      const previous = sections[cell.section - 1]!;
      const lastRowStart = previous.start + (rowCount(previous, cols) - 1) * cols;
      return Math.min(lastRowStart + cell.column, previous.start + previous.count - 1);
    }
    case 'down': {
      if (cell.row < rowCount(section, cols) - 1) {
        const candidate = index + cols;
        return candidate < section.start + section.count ? candidate : index;
      }
      const next = sections[cell.section + 1];
      if (!next) return index;
      return Math.min(next.start + cell.column, next.start + next.count - 1);
    }
    default:
      return index;
  }
}
