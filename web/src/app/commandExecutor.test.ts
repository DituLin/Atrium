/**
 * W-302 — the command executor against design §6.5 / §7.2 and PRD 5.3.
 */

import { describe, expect, it, vi } from 'vitest';

import type { CommandAckPayload, ScreenCommand } from '../types/ws';
import { createCommandExecutor } from './commandExecutor';
import { appReducer, createInitialState } from './state';
import type { AppAction, AppState } from './state';

function makeStore(): { get: () => AppState; dispatch: (action: AppAction) => void } {
  let state = createInitialState(0);
  return {
    get: () => state,
    dispatch: (action) => {
      state = appReducer(state, action);
    },
  };
}

function command(overrides: Partial<ScreenCommand> = {}): ScreenCommand {
  return {
    command_id: 'cmd_1',
    sequence: 1,
    kind: 'navigate',
    payload: { route: 'photos', collection: 'captured_today' },
    issued_at: '2026-09-06T10:00:00Z',
    // Deliberately in the past: the client must never judge expiry itself.
    expires_at: '1999-01-01T00:00:00Z',
    ...overrides,
  };
}

function harness(refresh: () => Promise<void> = () => Promise.resolve()) {
  const store = makeStore();
  const acks: CommandAckPayload[] = [];
  const refreshRoute = vi.fn(refresh);
  const executor = createCommandExecutor({
    getRouter: () => store.get().router,
    dispatch: store.dispatch,
    sendAck: (ack) => acks.push(ack),
    refreshRoute,
  });
  return { store, acks, executor, refreshRoute };
}

describe('navigate (W-302)', () => {
  it('applies a whitelisted route and acks applied with the final route', () => {
    const { store, acks, executor } = harness();
    executor.execute(command());
    expect(store.get().router.route).toEqual({ name: 'photos', collection: 'captured_today' });
    expect(store.get().router.appliedSequence).toBe(1);
    expect(acks).toEqual([
      { command_id: 'cmd_1', status: 'applied', route: { name: 'photos', collection: 'captured_today' } },
    ]);
  });

  it('rejects a route outside the whitelist and keeps the current page', () => {
    const { store, acks, executor } = harness();
    executor.execute(command({ payload: { route: 'admin' } }));
    expect(store.get().router.route).toEqual({ name: 'dashboard' });
    expect(store.get().router.appliedSequence).toBe(0);
    expect(acks[0]).toEqual({
      command_id: 'cmd_1',
      status: 'failed',
      route: { name: 'dashboard' },
      error_code: 'invalid_route',
    });
  });

  it('rejects an unknown collection', () => {
    const { acks, executor } = harness();
    executor.execute(command({ payload: { route: 'photos', collection: 'secret' } }));
    expect(acks[0]?.error_code).toBe('invalid_route');
  });
});

describe('sequence and duplicate rules (design §6.5)', () => {
  it('acks superseded for a sequence that is not newer, without navigating', () => {
    const { store, acks, executor } = harness();
    executor.execute(command({ command_id: 'cmd_5', sequence: 5 }));
    executor.execute(
      command({ command_id: 'cmd_3', sequence: 3, payload: { route: 'dashboard' } }),
    );
    expect(store.get().router.route).toEqual({ name: 'photos', collection: 'captured_today' });
    expect(store.get().router.appliedSequence).toBe(5);
    expect(acks[1]).toEqual({
      command_id: 'cmd_3',
      status: 'failed',
      route: { name: 'photos', collection: 'captured_today' },
      error_code: 'superseded',
    });
  });

  it('ignores a re-delivered command_id entirely — no second ack', () => {
    const { acks, executor } = harness();
    executor.execute(command());
    executor.execute(command());
    expect(acks).toHaveLength(1);
  });

  it('ignores a re-delivered command that was rejected the first time', () => {
    const { acks, executor } = harness();
    const stale = command({ command_id: 'cmd_0', sequence: 0, payload: { route: 'dashboard' } });
    executor.execute(stale);
    executor.execute(stale);
    expect(acks).toHaveLength(1);
    expect(acks[0]?.error_code).toBe('superseded');
  });
});

describe('show (W-302)', () => {
  it('pauses the slideshow, holds the ack, and applies on <img onload>', () => {
    const { store, acks, executor } = harness();
    executor.execute(command({ command_id: 'cmd_s', kind: 'show', payload: { photo_id: 'ph_9' } }));

    expect(store.get().router.route).toEqual({ name: 'photo', photoId: 'ph_9' });
    expect(store.get().slideshow.paused).toBe(true);
    // No ack yet, and the sequence has NOT moved: §6.5 applies a sequence only
    // when the client acks `applied`.
    expect(acks).toHaveLength(0);
    expect(store.get().router.appliedSequence).toBe(0);

    executor.settleRender('ph_9', 'ready');
    expect(acks).toEqual([
      {
        command_id: 'cmd_s',
        status: 'applied',
        route: { name: 'photo', photo_id: 'ph_9' },
        resource_id: 'ph_9',
      },
    ]);
    expect(store.get().router.appliedSequence).toBe(1);
  });

  it('does not settle on some other photo rendering', () => {
    const { acks, executor } = harness();
    executor.execute(command({ command_id: 'cmd_s', kind: 'show', payload: { photo_id: 'ph_9' } }));
    executor.settleRender('ph_1', 'ready');
    expect(acks).toHaveLength(0);
    expect(executor.pending()?.resourceId).toBe('ph_9');
  });

  it('acks photo_unavailable when the photo turns out to be missing', () => {
    const { store, acks, executor } = harness();
    executor.execute(command({ command_id: 'cmd_s', kind: 'show', payload: { photo_id: 'ph_9' } }));
    executor.settleRender(null, 'missing');
    expect(acks[0]).toEqual({
      command_id: 'cmd_s',
      status: 'failed',
      route: { name: 'photo', photo_id: 'ph_9' },
      resource_id: 'ph_9',
      error_code: 'photo_unavailable',
    });
    // A failed show must not raise the applied sequence.
    expect(store.get().router.appliedSequence).toBe(0);
  });

  it('rejects a payload with no usable photo id', () => {
    const { acks, executor } = harness();
    executor.execute(command({ kind: 'show', payload: { photo_id: '../etc/passwd' } }));
    expect(acks[0]?.error_code).toBe('invalid_payload');
  });

  it('keeps the collection when the show arrives on a collection page', () => {
    const { store, acks, executor } = harness();
    executor.execute(command({ command_id: 'cmd_n', sequence: 1 }));
    executor.execute(
      command({ command_id: 'cmd_s', sequence: 2, kind: 'show', payload: { photo_id: 'ph_2' } }),
    );
    executor.settleRender('ph_2', 'ready');
    expect(store.get().router.route).toEqual({
      name: 'photo',
      photoId: 'ph_2',
      collection: 'captured_today',
    });
    expect(acks[1]?.route).toEqual({
      name: 'photo',
      photo_id: 'ph_2',
      collection: 'captured_today',
    });
  });
});

describe('refresh (W-302)', () => {
  it('re-reads the current route, keeps the page and the pause state, acks after', async () => {
    let resolve = (): void => {};
    const gate = new Promise<void>((r) => {
      resolve = r;
    });
    const { store, acks, executor, refreshRoute } = harness(() => gate);
    store.dispatch({ type: 'slideshow.pause' });

    executor.execute(command({ command_id: 'cmd_r', kind: 'refresh', payload: {} }));
    expect(refreshRoute).toHaveBeenCalledWith({ name: 'dashboard' });
    // The ack waits for the data, not for the decision.
    expect(acks).toHaveLength(0);

    resolve();
    await gate;
    await Promise.resolve();

    expect(acks).toEqual([
      { command_id: 'cmd_r', status: 'applied', route: { name: 'dashboard' } },
    ]);
    expect(store.get().router.route).toEqual({ name: 'dashboard' });
    expect(store.get().slideshow.paused).toBe(true);
    expect(store.get().router.appliedSequence).toBe(1);
  });

  it('acks failed when the re-read fails', async () => {
    const { acks, executor } = harness(() => Promise.reject(new Error('offline')));
    executor.execute(command({ command_id: 'cmd_r', kind: 'refresh', payload: {} }));
    await Promise.resolve();
    await Promise.resolve();
    expect(acks[0]).toEqual({
      command_id: 'cmd_r',
      status: 'failed',
      route: { name: 'dashboard' },
      error_code: 'render_failed',
    });
  });
});
