/**
 * W-304 — scripted reconnect: `/home` first, then the route is kept only while
 * it is still valid against the fresh snapshot.
 */

import { describe, expect, it, vi } from 'vitest';

import { ApiClient, ApiError } from '../core/api';
import type { HomeResponse } from '../types/api';
import type { AppRoute } from './router';
import { syncSnapshot } from './snapshotSync';
import { appReducer, createInitialState } from './state';
import type { AppAction, AppState } from './state';

const HOME: HomeResponse = {
  schema_version: 1,
  server_time: '2026-09-06T12:00:00Z',
  version: 3,
  home: { name: 'Demo Home', timezone: 'Asia/Singapore' },
  widgets: [],
};

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

interface Scenario {
  homeStatus?: number;
  photoStatus?: number;
}

function harness(route: AppRoute, reconnected: boolean, scenario: Scenario = {}) {
  let state: AppState = createInitialState(0);
  const dispatch = (action: AppAction): void => {
    state = appReducer(state, action);
  };
  if (reconnected) {
    // A live session that has already rendered a snapshot and applied a command.
    dispatch({ type: 'connection.snapshotOk' });
    dispatch({ type: 'router.commandApplied', route, sequence: 11, commandId: 'cmd_before' });
  } else {
    dispatch({ type: 'router.navigate', route });
  }
  const calls: string[] = [];
  const fetchImpl = vi.fn((input: string) => {
    calls.push(input);
    if (input.startsWith('/api/v1/home')) {
      const status = scenario.homeStatus ?? 200;
      return Promise.resolve(status === 200 ? jsonResponse(HOME) : jsonResponse({ error: { code: 'unauthorized', message: 'no' } }, status));
    }
    const status = scenario.photoStatus ?? 200;
    return Promise.resolve(
      status === 200
        ? jsonResponse({ item: { id: 'ph_1' } })
        : jsonResponse({ error: { code: 'not_found', message: 'gone' } }, status),
    );
  });
  const api = new ApiClient({ fetchImpl });
  return { api, calls, get: () => state, dispatch };
}

describe('preflight order (W-304)', () => {
  it('fetches /home before anything else and records the snapshot', async () => {
    const h = harness({ name: 'dashboard' }, false);
    await syncSnapshot({ api: h.api, dispatch: h.dispatch, getState: h.get });
    expect(h.calls[0]).toBe('/api/v1/home');
    expect(h.get().home?.version).toBe(3);
    expect(h.get().connection.hasSnapshot).toBe(true);
  });

  it('propagates a 401 so the connection machine can route to pairing', async () => {
    const h = harness({ name: 'dashboard' }, false, { homeStatus: 401 });
    await expect(
      syncSnapshot({ api: h.api, dispatch: h.dispatch, getState: h.get }),
    ).rejects.toBeInstanceOf(ApiError);
  });
});

describe('route survival (W-304)', () => {
  it('cold start stays on the dashboard and validates no photo', async () => {
    const h = harness({ name: 'dashboard' }, false);
    await syncSnapshot({ api: h.api, dispatch: h.dispatch, getState: h.get });
    expect(h.get().router.route).toEqual({ name: 'dashboard' });
    expect(h.calls.filter((c) => c.startsWith('/api/v1/photos/'))).toHaveLength(0);
  });

  it('keeps a collection route without re-validating it', async () => {
    const route: AppRoute = { name: 'photos', collection: 'captured_today' };
    const h = harness(route, true);
    await syncSnapshot({ api: h.api, dispatch: h.dispatch, getState: h.get });
    expect(h.get().router.route).toEqual(route);
    expect(h.calls).toEqual(['/api/v1/home']);
  });

  it('keeps a photo route whose photo is still there', async () => {
    const route: AppRoute = { name: 'photo', photoId: 'ph_1', collection: 'recent' };
    const h = harness(route, true);
    await syncSnapshot({ api: h.api, dispatch: h.dispatch, getState: h.get });
    expect(h.calls[1]).toBe('/api/v1/photos/ph_1');
    expect(h.get().router.route).toEqual(route);
  });

  it('falls back to the dashboard when the photo is gone', async () => {
    const route: AppRoute = { name: 'photo', photoId: 'ph_1' };
    const h = harness(route, true, { photoStatus: 404 });
    await syncSnapshot({ api: h.api, dispatch: h.dispatch, getState: h.get });
    expect(h.get().router.route).toEqual({ name: 'dashboard' });
  });

  it('keeps the photo when the check failed for transport reasons', async () => {
    const route: AppRoute = { name: 'photo', photoId: 'ph_1' };
    const h = harness(route, true, { photoStatus: 503 });
    await syncSnapshot({ api: h.api, dispatch: h.dispatch, getState: h.get });
    expect(h.get().router.route).toEqual(route);
  });

  it('does not stomp on a navigation that happened while the preflight ran', async () => {
    const route: AppRoute = { name: 'photo', photoId: 'ph_1' };
    const h = harness(route, true, { photoStatus: 404 });
    const running = syncSnapshot({ api: h.api, dispatch: h.dispatch, getState: h.get });
    h.dispatch({ type: 'router.navigate', route: { name: 'photos', collection: 'all' } });
    await running;
    expect(h.get().router.route).toEqual({ name: 'photos', collection: 'all' });
  });

  it('keeps the applied sequence across the reconnect, so old commands are superseded', async () => {
    const h = harness({ name: 'photos', collection: 'recent' }, true);
    await syncSnapshot({ api: h.api, dispatch: h.dispatch, getState: h.get });
    expect(h.get().router.appliedSequence).toBe(11);
  });
});
