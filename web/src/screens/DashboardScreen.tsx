/**
 * Home (redesign "首页 · 常驻" and "首页 · 唤出菜单"). At rest the family photo
 * fills the screen with a quiet caption: date, time, one family notice and a
 * status line. A direction key opens the destination menu; OK on the photo
 * opens it full screen; Back closes the menu (see AppProvider).
 */

import type { ReactElement } from 'react';
import { useCallback, useEffect, useRef, useState } from 'react';

import { showsReconnectingBanner } from '../app/connection';
import { useApp } from '../app/context';
import { clockOptions, nasWidget, photoWidget } from '../app/homeSelect';
import { DEFAULT_COLLECTION } from '../app/router';
import type { AppRoute } from '../app/router';
import { formatClock, isTimeUnverified, serverNow } from '../core/clock';
import { WidgetBoundary } from '../ui/ErrorBoundary';
import { PhotoPane } from '../widgets/PhotoPane';
import { HomeMenu, ICONS } from './home/HomeMenu';
import type { HomeMenuCard } from './home/HomeMenu';
import { homeDateLine, homeLunarLine, homeNotices, homeStatus } from './home/homeSummary';

export const HINT_VISIBLE_MS = 10_000;

export function DashboardScreen(): ReactElement {
  const { state, dispatch } = useApp();
  // Coming back from a page opened through the menu reopens it on that card.
  // Home remounts on every return, so the router's restore target is read once.
  const [menu, setMenu] = useState<{ open: boolean; index: number }>(() => {
    const focus = state.router.restoreFocus;
    const index = focus?.startsWith('home-') ? CARD_IDS.indexOf(focus.slice(5) as CardId) : -1;
    return index >= 0 ? { open: true, index } : { open: false, index: 1 };
  });
  const [hint, setHint] = useState(true);
  const hero = useRef<HTMLElement | null>(null);

  const closeMenu = useCallback(() => {
    setMenu(current => ({ ...current, open: false }));
    document.querySelector<HTMLElement>('[data-home-hero]')?.focus();
  }, []);

  useEffect(() => {
    if (!hint || menu.open) return;
    const timer = setTimeout(() => setHint(false), HINT_VISIBLE_MS);
    return () => clearTimeout(timer);
  }, [hint, menu.open]);

  const options = clockOptions(state.home);
  const now = serverNow(state.clock, state.nowMs);
  const clock = formatClock(now, options);
  const photo = photoWidget(state.home);
  const status = homeStatus(state.connection.status, nasWidget(state.home)?.sources ?? []);
  const notices = homeNotices(state.overview.snapshot, now, state.overview.status === 'stale');
  const lunar = homeLunarLine(now, options);

  const go = (id: CardId, route: AppRoute) => () =>
    dispatch({ type: 'router.navigate', sourceFocus: `home-${id}`, route });
  const cards: HomeMenuCard[] = [
    { id: 'photo', name: '这张照片', meta: '全屏查看', icon: ICONS.photo, onOpen: () => hero.current?.click() },
    { id: 'media', name: '影像', meta: photo ? `${photo.totals.ready} 张照片` : '照片与视频', icon: ICONS.media, onOpen: go('media', { name: 'photos', collection: DEFAULT_COLLECTION }) },
    { id: 'today', name: '今日', meta: notices.length ? `${notices.length} 条家庭提示` : lunar ?? '家庭概览', icon: ICONS.today, onOpen: go('today', { name: 'briefing' }) },
    { id: 'house', name: '房屋', meta: status.tone === 'ok' ? '中枢正常' : '需要留意', icon: ICONS.house, onOpen: go('house', { name: 'house' }) },
    { id: 'settings', name: '设置', meta: '连接与照片来源', icon: ICONS.settings, onOpen: go('settings', { name: 'settings' }) },
  ];

  return (
    <div className={`screen-home surface--ink${menu.open ? ' screen-home--menu' : ''}`}>
      <WidgetBoundary name="photo">
        <PhotoPane payload={photo} interactive
          onFocus={() => { if (menu.open) setMenu(current => ({ ...current, open: false })); setHint(true); hero.current = document.querySelector<HTMLElement>('[data-home-hero]'); }}
          onDirection={() => setMenu({ open: true, index: 1 })} />
      </WidgetBoundary>

      <header className="home__top">
        <span className="home__brand serif">ATRIUM<span className="home__brand-cn">中庭</span></span>
        {menu.open ? (
          <span className="home__top-clock"><span>{homeDateLine(now, options)}</span><span className="serif home__top-time">{clock.time}</span></span>
        ) : hint ? (
          <span className="home__hint"><span className="home__key">OK</span>看这张照片<span className="home__sep" />方向键打开菜单</span>
        ) : null}
      </header>

      {showsReconnectingBanner(state.connection) ? (
        <div className="home__reconnect" role="status">
          <span className="dot dot--warn" />暂时连不上家里的服务<span className="home__sep" /><span className="muted">正在重试 · 画面已保留</span>
        </div>
      ) : null}

      {!menu.open ? (
        <section className="home__clock" aria-label="时钟">
          <div className="home__date serif">{homeDateLine(now, options)}</div>
          <div className="home__time serif">{clock.time}</div>
          {isTimeUnverified(state.clock, state.nowMs) ? <div className="home__unverified" role="status">时间未经核验</div> : null}
          {notices[0] ? (
            <div className="home__notice"><span className="serif home__notice-label">家庭提示</span><span>{notices[0]}</span></div>
          ) : null}
        </section>
      ) : null}

      {!menu.open ? (
        <p className="home__status" aria-label="系统状态"><span className={`dot dot--${status.tone}`} />{status.text}</p>
      ) : null}

      {menu.open ? <HomeMenu key={`menu-${menu.index}`} cards={cards} initialIndex={menu.index} onClose={closeMenu} /> : null}
    </div>
  );
}

const CARD_IDS = ['photo', 'media', 'today', 'house', 'settings'] as const;
type CardId = typeof CARD_IDS[number];
