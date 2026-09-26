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

const ENTRIES: readonly { id: MediaEntry; label: string }[] = [
  { id: 'recent', label: COLLECTION_LABELS.recent },
  { id: 'captured_today', label: COLLECTION_LABELS.captured_today },
  { id: 'random', label: COLLECTION_LABELS.random },
  { id: 'all', label: COLLECTION_LABELS.all },
  { id: 'videos', label: '视频' },
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
    <aside className="media-rail">
      <h1 className="media-rail__title serif">影像</h1>
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
              <span className="media-rail__dot" aria-hidden="true" />
              <span className="media-rail__label">{entry.label}</span>
              {selected && props.count ? <span className="media-rail__count" aria-hidden="true">{props.count}</span> : null}
            </RemoteButton>
          );
        })}
      </div>
    </aside>
  );
}
