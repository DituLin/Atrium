/**
 * Remote handling shared by the scrollable reading regions of the paper pages:
 * Up/Down scroll by most of a screen and leave the region only at its edges,
 * so long content is always reachable without a pointer.
 */

import type { KeyboardEvent } from 'react';

import { mapRemoteKey } from '../../ui/keys';

export interface ReadingEdges {
  left?: () => void;
  right?: () => void;
  /** Up while already scrolled to the top (usually the top navigation). */
  top?: () => void;
  /** Down while already scrolled to the bottom. */
  bottom?: () => void;
}

export function onReadingKey(event: KeyboardEvent<HTMLElement>, edges: ReadingEdges): void {
  const key = mapRemoteKey(event);
  if (!key || key === 'back' || key === 'enter') return;
  event.preventDefault();
  event.stopPropagation();
  const element = event.currentTarget;
  if (key === 'left') edges.left?.();
  if (key === 'right') edges.right?.();
  if (key === 'up') {
    if (element.scrollTop <= 0) edges.top?.();
    else element.scrollTop = Math.max(0, element.scrollTop - element.clientHeight * 0.7);
  }
  if (key === 'down') {
    if (element.scrollTop + element.clientHeight >= element.scrollHeight - 1) edges.bottom?.();
    else element.scrollTop += element.clientHeight * 0.7;
  }
}

/** Focus the element that opened a child page, falling back to `fallback`. */
export function restoreReturnFocus(restoreFocus: string | undefined, fallback: HTMLElement | null): void {
  const source = restoreFocus
    ? Array.from(document.querySelectorAll<HTMLElement>('[data-return-focus]')).find(element => element.dataset.returnFocus === restoreFocus)
    : null;
  (source ?? fallback)?.focus();
}
