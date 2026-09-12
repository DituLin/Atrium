import { useRef } from 'react';
import { useApp } from '../app/context';
import { DEFAULT_COLLECTION } from '../app/router';
import { RemoteButton } from './RemoteButton';

const LINKS = [['dashboard', '首页'], ['photos', '照片'], ['briefing', '今日'], ['house', '房屋'], ['settings', '设置']] as const;
export function PrimaryNav({ onUp }: { onUp: () => void }) {
  const { state, dispatch } = useApp();
  const buttons = useRef<(HTMLButtonElement | null)[]>([]);
  return <nav className="primary-nav" aria-label="主要导航">
    {LINKS.map(([name, label], index) => <RemoteButton key={name}
      ref={element => { buttons.current[index] = element; }}
      className="primary-nav__item" data-nav={name} data-return-focus={`nav-${name}`}
      aria-current={state.router.route.name === name ? 'page' : undefined}
      onDirection={key => {
        if (key === 'up') onUp();
        if (key === 'left' || key === 'right') buttons.current[Math.max(0, Math.min(LINKS.length - 1, index + (key === 'left' ? -1 : 1)))]?.focus();
      }}
      onClick={() => dispatch({ type: 'router.navigate', sourceFocus: `nav-${name}`, route: name === 'photos'
        ? { name, collection: DEFAULT_COLLECTION } : { name } })}>{label}</RemoteButton>)}
    <span className="primary-nav__hint">方向键移动 · 确认打开 · 返回上一级</span>
  </nav>;
}

export function focusPrimaryNav(name: 'dashboard' | 'photos' | 'briefing' | 'house' | 'settings'): void {
  document.querySelector<HTMLButtonElement>(`[data-nav="${name}"]`)?.focus();
}

export function Masthead() {
  return <header className="masthead"><span className="masthead__brand">ATRIUM</span><span>留一方光景</span></header>;
}
