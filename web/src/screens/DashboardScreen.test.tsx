import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { AppContext } from '../app/context';
import { createInitialState } from '../app/state';
import { ApiClient } from '../core/api';
import { useSlideshow } from '../widgets/useSlideshow';
import { DashboardScreen } from './DashboardScreen';

vi.mock('../widgets/useSlideshow', () => ({ useSlideshow: vi.fn() }));
function setup() {
  const dispatch = vi.fn();
  vi.mocked(useSlideshow).mockReturnValue({ status: 'playing', previous: null, item: null,
    shown: { id: 'actually_visible', src: '/visible', element: new Image() }, fixed: false, count: 2 });
  render(<AppContext.Provider value={{ house: { load: async () => {}, invalidate: () => {} }, state: createInitialState(0), api: new ApiClient(),
    dispatch, goBack: () => {}, clientVersion: 'test' }}><DashboardScreen /></AppContext.Provider>);
  return dispatch;
}

describe('home remote navigation', () => {
  it('initially focuses the photo and opens exactly the decoded visible slide', () => {
    const dispatch = setup();
    const hero = screen.getByRole('button', { name: '打开当前照片' });
    expect(document.activeElement).toBe(hero);
    fireEvent.keyDown(hero, { key: 'Enter' });
    expect(dispatch).toHaveBeenCalledExactlyOnceWith({ type: 'router.navigate', route: { name: 'photo', photoId: 'actually_visible' } });
  });
  it('moves right to status and down to navigation without navigating on focus', () => {
    const dispatch = setup();
    const hero = screen.getByRole('button', { name: '打开当前照片' });
    fireEvent.keyDown(hero, { key: 'ArrowRight' });
    expect(document.activeElement).toBe(screen.getByRole('button', { name: /查看状态/ }));
    expect(dispatch).not.toHaveBeenCalled();
    fireEvent.keyDown(document.activeElement!, { key: 'ArrowLeft' });
    expect(document.activeElement).toBe(hero);
    fireEvent.keyDown(hero, { key: 'ArrowDown' });
    const home = screen.getByRole('button', { name: '首页' });
    expect(document.activeElement).toBe(home);
    fireEvent.keyDown(home, { key: 'ArrowRight' });
    expect(document.activeElement).toBe(screen.getByRole('button', { name: '照片' }));
    expect(dispatch).not.toHaveBeenCalled();
  });
});
