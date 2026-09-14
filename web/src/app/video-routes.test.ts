import { expect, it } from 'vitest';
import { backRoute, initialRouterState, parseRoutePath, routerReducer, toRouteState } from './router';
it('parses local video routes and reports the video identity', () => {
  expect(parseRoutePath('videos')).toEqual({ name: 'videos' });
  const route = parseRoutePath('video/v01');
  expect(route).toEqual({ name: 'video', videoId: 'v01' });
  expect(toRouteState(route!)).toEqual({ name: 'video', video_id: 'v01' });
  expect(backRoute(route!)).toEqual({ name: 'videos' });
  for (const path of ['video', 'video/a/b', 'video/..', 'videos/extra']) expect(parseRoutePath(path)).toBeNull();
});
it('returns through the video list to its origin without pushing the player into history', () => {
  let state = routerReducer(initialRouterState, { type: 'router.navigate', route: parseRoutePath('videos')!, sourceFocus: 'nav-videos' });
  state = routerReducer(state, { type: 'router.navigate', route: parseRoutePath('video/v01')! });
  state = routerReducer(state, { type: 'router.back' });
  expect(state.route).toEqual({ name: 'videos' });
  state = routerReducer(state, { type: 'router.back' });
  expect(state.route).toEqual({ name: 'dashboard' });
  expect(state.restoreFocus).toBe('nav-videos');
});
