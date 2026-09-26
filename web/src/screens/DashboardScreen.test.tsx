import { act, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { AppContext } from '../app/context';
import { createInitialState } from '../app/state';
import { ApiClient } from '../core/api';
import { useSlideshow } from '../widgets/useSlideshow';
import { DashboardScreen } from './DashboardScreen';
import { MENU_IDLE_MS } from './home/HomeMenu';

vi.mock('../widgets/useSlideshow', () => ({ useSlideshow: vi.fn() }));
afterEach(() => vi.useRealTimers());

function setup() {
  const dispatch = vi.fn();
  vi.mocked(useSlideshow).mockReturnValue({ status: 'playing', previous: null, item: null,
    shown: { id: 'actually_visible', src: '/visible', element: new Image() }, fixed: false, count: 2 });
  render(<AppContext.Provider value={{ overview: { load: async () => {}, invalidate: () => {} }, house: { load: async () => {}, invalidate: () => {} }, state: createInitialState(0), api: new ApiClient(),
    dispatch, goBack: () => {}, clientVersion: 'test' } as never}><DashboardScreen /></AppContext.Provider>);
  return dispatch;
}
const hero = () => screen.getByRole('button', { name: '打开当前照片' });

describe('home remote navigation', () => {
  it('initially focuses the photo and opens exactly the decoded visible slide', () => {
    const dispatch = setup();
    expect(document.activeElement).toBe(hero());
    fireEvent.keyDown(hero(), { key: 'Enter' });
    expect(dispatch).toHaveBeenCalledExactlyOnceWith({ type: 'router.navigate', route: { name: 'photo', photoId: 'actually_visible' } });
  });

  it('opens the menu on 影像 with a direction key and moves without navigating', () => {
    const dispatch = setup();
    fireEvent.keyDown(hero(), { key: 'ArrowDown' });
    expect(document.activeElement).toBe(screen.getByRole('button', { name: /^影像/ }));
    fireEvent.keyDown(document.activeElement!, { key: 'ArrowRight' });
    expect(document.activeElement).toBe(screen.getByRole('button', { name: /^今日/ }));
    expect(dispatch).not.toHaveBeenCalled();
  });

  it('navigates from a card with the card as the return focus', () => {
    const dispatch = setup();
    fireEvent.keyDown(hero(), { key: 'ArrowRight' });
    fireEvent.keyDown(screen.getByRole('button', { name: /^影像/ }), { key: 'Enter' });
    expect(dispatch).toHaveBeenCalledWith({ type: 'router.navigate', sourceFocus: 'home-media', route: { name: 'photos', collection: 'recent' } });
  });

  it('closes the menu with Up and after the idle timeout, returning focus to the photo', () => {
    vi.useFakeTimers();
    setup();
    fireEvent.keyDown(hero(), { key: 'ArrowDown' });
    fireEvent.keyDown(document.activeElement!, { key: 'ArrowUp' });
    expect(screen.queryByRole('navigation', { name: '主菜单' })).toBeNull();
    expect(document.activeElement).toBe(hero());
    fireEvent.keyDown(hero(), { key: 'ArrowDown' });
    act(() => { vi.advanceTimersByTime(MENU_IDLE_MS + 10); });
    expect(screen.queryByRole('navigation', { name: '主菜单' })).toBeNull();
    expect(document.activeElement).toBe(hero());
  });
});
