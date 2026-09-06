import { describe, expect, it } from 'vitest';

import type { HomeResponse, PhotoItem } from '../types/api';
import { appReducer, createInitialState, selectScreen } from './state';

const HOME: HomeResponse = {
  schema_version: 1,
  server_time: '2026-09-05T02:00:00Z',
  version: 3,
  home: { name: 'Demo Home', timezone: 'Asia/Singapore' },
  widgets: [
    {
      type: 'clock',
      payload: {
        timezone: 'Asia/Singapore',
        server_time: '2026-09-05T02:00:00Z',
        utc_offset_seconds: 28800,
      },
    },
  ],
};

const PHOTO = {
  id: 'p1',
  preview: { status: 'ready', width: 1, height: 1 },
} as unknown as PhotoItem;

const HOME_WITH_PHOTO: HomeResponse = {
  ...HOME,
  widgets: [
    ...HOME.widgets,
    {
      type: 'photo',
      payload: {
        slideshow_interval_seconds: 20,
        totals: { ready: 2, pending_preview: 0, unsupported: 0 },
        new_today: 0,
        captured_today: 0,
        unknown_captured: 0,
        baseline: { status: 'done', completed_at: null },
        index: {
          state: 'idle',
          progress: { seen: 2, indexed: 2, pending_preview: 0 },
          last_scan_at: null,
        },
      },
    },
  ],
};

describe('app state', () => {
  it('syncs the clock from a home snapshot', () => {
    const state = appReducer(createInitialState(0), {
      type: 'app.homeLoaded',
      home: HOME,
      receivedAt: Date.parse('2026-09-05T02:00:05Z'),
    });
    expect(state.clock.offsetMs).toBe(-5000);
    expect(state.home?.home.name).toBe('Demo Home');
  });

  it('takes the slideshow interval from the photo widget', () => {
    const state = appReducer(createInitialState(0), {
      type: 'app.homeLoaded',
      home: HOME_WITH_PHOTO,
      receivedAt: 0,
    });
    expect(state.slideshow.intervalMs).toBe(20_000);
  });

  it('purges photo state and the snapshot when the auth cache expires', () => {
    let state = appReducer(createInitialState(0), {
      type: 'app.homeLoaded',
      home: HOME,
      receivedAt: 0,
    });
    state = appReducer(state, { type: 'slideshow.roundStarted', seed: 's' });
    state = appReducer(state, {
      type: 'slideshow.pageLoaded',
      seed: 's',
      items: [PHOTO],
      nextCursor: null,
    });
    state = appReducer(state, { type: 'photos.open', collection: 'recent' });
    expect(state.slideshow.round).toHaveLength(1);
    state = appReducer(state, { type: 'app.authExpired' });
    expect(state.slideshow.round).toHaveLength(0);
    expect(state.collection.collection).toBeNull();
    expect(state.viewer.photoId).toBeNull();
    expect(state.home).toBeNull();
    expect(selectScreen(state)).toBe('connect');
  });

  it('does not accept new photos while expired', () => {
    let state = appReducer(createInitialState(0), { type: 'app.authExpired' });
    state = appReducer(state, { type: 'slideshow.roundStarted', seed: 's' });
    state = appReducer(state, {
      type: 'slideshow.pageLoaded',
      seed: 's',
      items: [PHOTO],
      nextCursor: null,
    });
    expect(state.slideshow.round).toHaveLength(0);
  });

  it('routes to pair on a 401 and back to the route after a claim', () => {
    let state = appReducer(createInitialState(0), { type: 'app.needsPairing' });
    expect(selectScreen(state)).toBe('pair');
    state = appReducer(state, { type: 'pair.claimed', screenName: 'Living room TV' });
    expect(state.needsPairing).toBe(false);
    // Still no snapshot, so the connect screen holds until /home succeeds.
    expect(selectScreen(state)).toBe('connect');
    state = appReducer(state, { type: 'app.homeLoaded', home: HOME, receivedAt: 0 });
    state = appReducer(state, { type: 'connection.snapshotOk' });
    state = appReducer(state, { type: 'connection.socketOpen' });
    expect(selectScreen(state)).toBe('dashboard');
  });

  it('keeps rendering the dashboard while reconnecting with a cached snapshot', () => {
    let state = appReducer(createInitialState(0), { type: 'app.homeLoaded', home: HOME, receivedAt: 0 });
    state = appReducer(state, { type: 'connection.snapshotOk' });
    state = appReducer(state, { type: 'connection.socketOpen' });
    state = appReducer(state, { type: 'connection.attemptFailed' });
    expect(selectScreen(state)).toBe('dashboard');
  });
});
