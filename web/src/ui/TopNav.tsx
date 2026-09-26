/**
 * Top navigation for paper information pages (首页 · 影像 · 今日 · 房屋 · 设置).
 * It sits above the content so page controls can never collide with it.
 *
 * Remote: Left/Right move between entries and stop at the edges, Down hands
 * focus back to the page (`onDown`), OK navigates. Pages reach it with Up from
 * their top edge through `focusTopNav`.
 */

import type { ReactElement } from 'react';
import { useRef } from 'react';

import { useApp } from '../app/context';
import { clockOptions } from '../app/homeSelect';
import { DEFAULT_COLLECTION } from '../app/router';
import type { AppRoute } from '../app/router';
import { formatClock, serverNow } from '../core/clock';
import { RemoteButton } from './RemoteButton';

export type TopNavEntry = 'dashboard' | 'media' | 'briefing' | 'house' | 'settings';

const ENTRIES: readonly { id: TopNavEntry; label: string; route: AppRoute }[] = [
  { id: 'dashboard', label: '首页', route: { name: 'dashboard' } },
  { id: 'media', label: '影像', route: { name: 'photos', collection: DEFAULT_COLLECTION } },
  { id: 'briefing', label: '今日', route: { name: 'briefing' } },
  { id: 'house', label: '房屋', route: { name: 'house' } },
  { id: 'settings', label: '设置', route: { name: 'settings' } },
];

/** Which entry a route belongs to; photos and videos are both 影像. */
export function topNavEntryFor(route: AppRoute): TopNavEntry | null {
  switch (route.name) {
    case 'dashboard': return 'dashboard';
    case 'photos': case 'photo': case 'videos': case 'video': return 'media';
    case 'briefing': return 'briefing';
    case 'house': return 'house';
    case 'settings': return 'settings';
    default: return null;
  }
}

export function focusTopNav(entry?: TopNavEntry): void {
  const target = entry
    ? document.querySelector<HTMLButtonElement>(`[data-topnav="${entry}"]`)
    : document.querySelector<HTMLButtonElement>('.topnav__item[aria-current="page"]');
  (target ?? document.querySelector<HTMLButtonElement>('.topnav__item'))?.focus();
}

export function TopNav(props: { onDown: () => void }): ReactElement {
  const { state, dispatch } = useApp();
  const buttons = useRef<(HTMLButtonElement | null)[]>([]);
  const current = topNavEntryFor(state.router.route);
  const time = formatClock(serverNow(state.clock, state.nowMs), clockOptions(state.home)).time;
  return (
    <header className="topnav">
      <span className="topnav__brand">ATRIUM</span>
      <nav className="topnav__items" aria-label="主要导航">
        {ENTRIES.map((entry, index) => (
          <RemoteButton
            key={entry.id}
            ref={element => { buttons.current[index] = element; }}
            className="topnav__item"
            data-topnav={entry.id}
            data-return-focus={`nav-${entry.id}`}
            aria-current={current === entry.id ? 'page' : undefined}
            onDirection={key => {
              if (key === 'down') props.onDown();
              if (key === 'left') buttons.current[Math.max(0, index - 1)]?.focus();
              if (key === 'right') buttons.current[Math.min(ENTRIES.length - 1, index + 1)]?.focus();
            }}
            onClick={() => {
              if (current === entry.id) { props.onDown(); return; }
              dispatch({ type: 'router.navigate', sourceFocus: `nav-${entry.id}`, route: entry.route });
            }}
          >
            {entry.label}
          </RemoteButton>
        ))}
      </nav>
      <span className="topnav__clock" aria-label="当前时间">{time}</span>
    </header>
  );
}
