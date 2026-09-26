import { describe, expect, it } from 'vitest';

import { ROUTE_NAMES } from '../types/ws';

import type { AppRoute } from './router';
import {
  backRoute,
  initialRouterState,
  parseRoutePath,
  routeToPath,
  routerReducer,
  toRouteState,
} from './router';

describe('route whitelist', () => {
  it('accepts the documented local routes', () => {
    expect(parseRoutePath('settings')).toEqual({ name: 'settings' });
    expect(toRouteState({ name: 'settings' })).toEqual({ name: 'settings' });
    expect(backRoute({ name: 'settings' })).toEqual({ name: 'dashboard' });
    expect(parseRoutePath('pair')).toEqual({ name: 'pair' });
    expect(parseRoutePath('connect')).toEqual({ name: 'connect' });
    expect(parseRoutePath('dashboard')).toEqual({ name: 'dashboard' });
    expect(parseRoutePath('/dashboard/')).toEqual({ name: 'dashboard' });
    expect(parseRoutePath('')).toEqual({ name: 'dashboard' });
    expect(parseRoutePath('photos/recent')).toEqual({ name: 'photos', collection: 'recent' });
    expect(parseRoutePath('photos/captured_today')).toEqual({
      name: 'photos',
      collection: 'captured_today',
    });
    expect(parseRoutePath('photo/01JABC')).toEqual({ name: 'photo', photoId: '01JABC' });
  });

  it('rejects everything else', () => {
    for (const bad of [
      'admin',
      'photos/secret',
      'photos/recent/extra',
      'photo',
      'photo/../etc',
      'photo/a/b',
      'photo/with space',
      '../../etc/passwd',
      'dashboard/x',
      '__proto__',
    ]) {
      expect(parseRoutePath(bad), bad).toBeNull();
    }
  });

  it('round-trips through routeToPath', () => {
    for (const path of ['pair', 'connect', 'dashboard', 'photos/all', 'photo/abc']) {
      const route = parseRoutePath(path);
      expect(route).not.toBeNull();
      expect(routeToPath(route!)).toBe(path);
    }
  });

  it('projects to the WS RouteState shape', () => {
    expect(toRouteState({ name: 'photos', collection: 'random' })).toEqual({
      name: 'photos',
      collection: 'random',
    });
    expect(toRouteState({ name: 'photo', photoId: 'p1', collection: 'all' })).toEqual({
      name: 'photo',
      photo_id: 'p1',
      collection: 'all',
    });
    expect(toRouteState({ name: 'dashboard' })).toEqual({ name: 'dashboard' });
  });
});

describe('back key', () => {
  it('walks photo → photos → dashboard and stops there', () => {
    expect(backRoute({ name: 'photo', photoId: 'p', collection: 'all' })).toEqual({
      name: 'photos',
      collection: 'all',
    });
    // Opened from the dashboard (no collection): Back goes straight home.
    expect(backRoute({ name: 'photo', photoId: 'p' })).toEqual({ name: 'dashboard' });
    expect(backRoute({ name: 'photos', collection: 'all' })).toEqual({ name: 'dashboard' });
    expect(backRoute({ name: 'dashboard' })).toEqual({ name: 'dashboard' });
  });
});

describe('router reducer', () => {
  it('keeps identity when the route does not change', () => {
    const state = routerReducer(initialRouterState, {
      type: 'router.navigate',
      route: { name: 'dashboard' },
    });
    expect(state).toBe(initialRouterState);
  });

  it('tracks the applied sequence monotonically', () => {
    let state = routerReducer(initialRouterState, {
      type: 'router.commandApplied',
      route: { name: 'photos', collection: 'recent' },
      sequence: 5,
      commandId: 'c1',
    });
    expect(state.appliedSequence).toBe(5);
    state = routerReducer(state, {
      type: 'router.commandApplied',
      route: { name: 'dashboard' },
      sequence: 3,
      commandId: 'c2',
    });
    expect(state.appliedSequence).toBe(5);
    expect(state.handledCommandIds).toEqual(['c1', 'c2']);
  });

  it('bounds the handled-command list', () => {
    let state = initialRouterState;
    for (let i = 0; i < 100; i += 1) {
      state = routerReducer(state, {
        type: 'router.commandRejected',
        sequence: i,
        commandId: `c${i}`,
      });
    }
    expect(state.handledCommandIds).toHaveLength(64);
    expect(state.handledCommandIds[state.handledCommandIds.length - 1]).toBe('c99');
  });
});

describe('RouteState shape (§9)', () => {
  it('only ever carries name / collection / photo_id, and omits what is absent', () => {
    const cases: AppRoute[] = [
      { name: 'dashboard' },
      { name: 'pair' },
      { name: 'connect' },
      { name: 'photos', collection: 'captured_today' },
      { name: 'photo', photoId: 'ph_1' },
      { name: 'photo', photoId: 'ph_1', collection: 'recent' },
    ];
    for (const route of cases) {
      const wire = toRouteState(route);
      expect(ROUTE_NAMES).toContain(wire.name);
      expect(Object.keys(wire).every((key) => ['name', 'collection', 'photo_id'].includes(key))).toBe(
        true,
      );
      // Absent fields are omitted, never sent as null/undefined.
      expect(JSON.parse(JSON.stringify(wire))).toEqual(wire);
    }
    expect(toRouteState({ name: 'photo', photoId: 'ph_1' })).toEqual({
      name: 'photo',
      photo_id: 'ph_1',
    });
  });
});

it('reports House locally and restores source focus through House, photos and settings', () => {
  expect(parseRoutePath('house')).toEqual({ name: 'house' });
  expect(toRouteState({ name: 'house' })).toEqual({ name: 'house' });
  expect(ROUTE_NAMES).toContain('house');
  let state = routerReducer(initialRouterState, { type: 'router.navigate', route: { name: 'house' }, sourceFocus: 'nav-house' });
  for (const route of [{ name: 'photos', collection: 'recent' }, { name: 'settings' }] as const) {
    state = routerReducer(state, { type: 'router.navigate', route, sourceFocus: `nav-${route.name}` });
    state = routerReducer(state, { type: 'router.back' });
    expect(state.route.name).toBe('house'); expect(state.restoreFocus).toBe(`nav-${route.name}`);
  }
  state = routerReducer(state, { type: 'router.back' });
  expect(state.route.name).toBe('dashboard'); expect(state.restoreFocus).toBe('nav-house');
  expect(backRoute({ name: 'house' })).toEqual({ name: 'dashboard' });
});

it('reports Briefing locally and restores source focus through Briefing, photos and settings', () => {
  expect(parseRoutePath('briefing')).toEqual({ name: 'briefing' });
  expect(toRouteState({ name: 'briefing' })).toEqual({ name: 'briefing' });
  expect(ROUTE_NAMES).toContain('briefing');
  let state = routerReducer(initialRouterState, { type: 'router.navigate', route: { name: 'briefing' }, sourceFocus: 'nav-briefing' });
  for (const route of [{ name: 'photos', collection: 'recent' }, { name: 'settings' }] as const) {
    state = routerReducer(state, { type: 'router.navigate', route, sourceFocus: `nav-${route.name}` });
    state = routerReducer(state, { type: 'router.back' });
    expect(state.route.name).toBe('briefing'); expect(state.restoreFocus).toBe(`nav-${route.name}`);
  }
  state = routerReducer(state, { type: 'router.back' });
  expect(state.route.name).toBe('dashboard'); expect(state.restoreFocus).toBe('nav-briefing');
  expect(backRoute({ name: 'briefing' })).toEqual({ name: 'dashboard' });
});

it('returns House -> Briefing -> Settings one layer at a time to each original entry', () => {
 let state = routerReducer(initialRouterState, { type: 'router.navigate', route: { name: 'house' }, sourceFocus: 'nav-house' });
 state = routerReducer(state, { type: 'router.navigate', route: { name: 'briefing' }, sourceFocus: 'nav-briefing' });
 state = routerReducer(state, { type: 'router.navigate', route: { name: 'settings' }, sourceFocus: 'nav-settings' });
 for (const [name, focus] of [['briefing', 'nav-settings'], ['house', 'nav-briefing'], ['dashboard', 'nav-house']]) {
  state = routerReducer(state, { type: 'router.back' }); expect(state.route.name).toBe(name); expect(state.restoreFocus).toBe(focus);
 }
 expect(state.returnStack).toEqual([]);
});

it('no longer has a calendar page; the lunar date lives on 今日', () => {
 expect(parseRoutePath('calendar')).toBeNull();
 expect(ROUTE_NAMES).not.toContain('calendar');
});

it('treats photos and videos as one library layer when switching the 影像 rail', () => {
  let state = routerReducer(initialRouterState, { type: 'router.navigate', route: { name: 'house' } });
  state = routerReducer(state, { type: 'router.navigate', route: { name: 'photos', collection: 'recent' }, sourceFocus: 'nav-media' });
  state = routerReducer(state, { type: 'router.navigate', route: { name: 'videos' } });
  state = routerReducer(state, { type: 'router.navigate', route: { name: 'photos', collection: 'all' } });
  expect(state.returnStack).toHaveLength(2);
  state = routerReducer(state, { type: 'router.back' });
  expect(state.route.name).toBe('house');
  expect(state.restoreFocus).toBe('nav-media');
});
