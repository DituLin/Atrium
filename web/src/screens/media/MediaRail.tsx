/**
 * The 影像 library's left rail, shared by the photo collections and the video
 * list so both routes read as one page with the right entry selected.
 *
 * Remote: Up/Down move between entries (Up from the first reaches the top
 * navigation), Right hands focus to the content, OK selects. Focusing an
 * entry never applies it; only OK does.
 */

import type { ReactElement } from 'react';
import { useRef } from 'react';

import type { PhotoCollection } from '../../types/api';
import type { RemoteKey } from '../../ui/keys';
import { RemoteButton } from '../../ui/RemoteButton';
import { focusTopNav } from '../../ui/TopNav';
import { COLLECTION_LABELS } from './viewerPosition';

export type MediaEntry = PhotoCollection | 'videos';

const ENTRIES: readonly { id: MediaEntry; label: string; icon: string }[] = [
  { id: 'recent', label: COLLECTION_LABELS.recent, icon: 'M12 7v5l3 2 M12 3a9 9 0 1 0 0 18a9 9 0 1 0 0-18' },
  { id: 'captured_today', label: COLLECTION_LABELS.captured_today, icon: 'M4 7h3l2-3h6l2 3h3v12H4z M12 10a3 3 0 1 0 0 6a3 3 0 1 0 0-6' },
  { id: 'random', label: COLLECTION_LABELS.random, icon: 'M4 7h3l10 10h3 M4 17h3l3-3 M14 10l3-3h3' },
  { id: 'all', label: COLLECTION_LABELS.all, icon: 'M4 4h7v7H4z M13 4h7v7h-7z M4 13h7v7H4z M13 13h7v7h-7z' },
  { id: 'videos', label: '视频', icon: 'M3 6h13v12H3z M16 10l5-3v10l-5-3' },
];

/** Focus the selected rail entry inside `root` (the current screen). */
export function focusMediaRail(root: ParentNode | null | undefined): void {
  root?.querySelector<HTMLElement>('.media-rail__entry[aria-selected="true"]')?.focus();
}

export interface MediaRailProps {
  selected: MediaEntry;
  /** Shown beside the selected entry only, and only when the API gave it. */
  count?: string | null;
  onSelect: (entry: MediaEntry) => void;
  onRight: () => void;
  /** Called before any direction is handled (e.g. to drop pending focus). */
  onKey?: (key: RemoteKey) => void;
}

export function MediaRail(props: MediaRailProps): ReactElement {
  const entries = useRef<(HTMLButtonElement | null)[]>([]);
  return (
    <aside className="media-rail" aria-label="影像合集">
      <div className="media-rail__entries" role="tablist" aria-orientation="vertical" aria-label="影像分类">
        {ENTRIES.map((entry, index) => {
          const selected = entry.id === props.selected;
          return (
            <RemoteButton
              key={entry.id}
              role="tab"
              aria-selected={selected}
              className="media-rail__entry"
              data-media-entry={entry.id}
              ref={element => { entries.current[index] = element; }}
              onClick={() => props.onSelect(entry.id)}
              onDirection={key => {
                props.onKey?.(key);
                if (key === 'up') { if (index === 0) focusTopNav(); else entries.current[index - 1]?.focus(); }
                if (key === 'down') entries.current[Math.min(ENTRIES.length - 1, index + 1)]?.focus();
                if (key === 'right') props.onRight();
              }}
            >
              <span className="media-rail__icon" aria-hidden="true"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round"><path d={entry.icon} /></svg></span>
              <span className="media-rail__label">{entry.label}</span>
              {selected && props.count ? <span className="media-rail__count" aria-hidden="true">{props.count}</span> : null}
            </RemoteButton>
          );
        })}
      </div>
    </aside>
  );
}
