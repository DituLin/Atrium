/**
 * The home menu (redesign "首页 · 唤出菜单"): a row of destination cards that
 * appears over the ambient photo when a direction key is pressed. It closes on
 * Back (AppProvider refocuses the photo), on Up, or after 12 s without input.
 */

import type { ReactElement } from 'react';
import { useEffect, useRef } from 'react';

import { RemoteButton } from '../../ui/RemoteButton';

export const MENU_IDLE_MS = 12_000;

export interface HomeMenuCard {
  id: string;
  name: string;
  meta: string;
  icon: string;
  onOpen: () => void;
}

export const ICONS = {
  photo: 'M4 5h16v14H4z M4 15l5-5 4 4 3-3 4 4',
  media: 'M3 6h13v12H3z M16 10l5-3v10l-5-3',
  today: 'M12 4v2 M12 18v2 M4 12h2 M18 12h2 M12 8a4 4 0 1 0 0 8a4 4 0 1 0 0-8',
  house: 'M4 11l8-7 8 7 M6 10v10h12V10 M10 20v-6h4v6',
  settings: 'M12 9a3 3 0 1 0 0 6a3 3 0 1 0 0-6 M12 3v3 M12 18v3 M3 12h3 M18 12h3 M5.6 5.6l2.1 2.1 M16.3 16.3l2.1 2.1 M5.6 18.4l2.1-2.1 M16.3 7.7l2.1-2.1',
} as const;

export function HomeMenu(props: {
  cards: readonly HomeMenuCard[];
  /** Index focused when the menu opens. */
  initialIndex: number;
  onClose: () => void;
}): ReactElement {
  const buttons = useRef<(HTMLButtonElement | null)[]>([]);
  const idle = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const { onClose } = props;
  const arm = () => {
    clearTimeout(idle.current);
    idle.current = setTimeout(onClose, MENU_IDLE_MS);
  };
  useEffect(() => {
    buttons.current[Math.min(props.initialIndex, props.cards.length - 1)]?.focus();
    arm();
    return () => clearTimeout(idle.current);
    // Opening focuses once; later card updates must not steal focus.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  return (
    // Capture phase: the cards stop arrow keys from bubbling, but every key
    // must still count as activity for the idle timer.
    <nav className="home-menu" aria-label="主菜单" onKeyDownCapture={arm}>
      <div className="home-menu__cards">
        {props.cards.map((card, index) => (
          <RemoteButton key={card.id} ref={element => { buttons.current[index] = element; }}
            className="home-menu__card" data-return-focus={`home-${card.id}`} aria-label={`${card.name}，${card.meta}`}
            onClick={card.onOpen}
            onDirection={key => {
              if (key === 'up') onClose();
              if (key === 'left') buttons.current[Math.max(0, index - 1)]?.focus();
              if (key === 'right') buttons.current[Math.min(props.cards.length - 1, index + 1)]?.focus();
            }}>
            <svg className="home-menu__icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
              <path d={card.icon} />
            </svg>
            <span className="home-menu__name serif">{card.name}</span>
            <span className="home-menu__meta">{card.meta}</span>
          </RemoteButton>
        ))}
      </div>
      <p className="hints hints--ink">
        <span>← → 选择</span><span>OK 进入</span><span>返回 收起菜单</span>
        <span className="hints__end">12 秒无操作自动收起</span>
      </p>
    </nav>
  );
}
