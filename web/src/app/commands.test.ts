import { describe, expect, it } from 'vitest';

import type { ScreenCommand } from '../types/ws';
import {
  decideCommand,
  failedAck,
  needsRenderConfirmation,
  pendingAckFor,
  resolvePendingAck,
} from './commands';
import { initialRouterState, routerReducer } from './router';

function command(over: Partial<ScreenCommand>): ScreenCommand {
  return {
    command_id: 'cmd_1',
    sequence: 1,
    kind: 'navigate',
    payload: { route: 'dashboard' },
    issued_at: '2026-09-05T00:00:00Z',
    expires_at: '2026-09-05T00:00:30Z',
    ...over,
  };
}

describe('client command rules (design §6.5)', () => {
  it('applies a navigate with a higher sequence', () => {
    const decision = decideCommand(initialRouterState, command({ payload: { route: 'photos', collection: 'all' } }));
    expect(decision).toEqual({
      kind: 'apply',
      route: { name: 'photos', collection: 'all' },
      command: expect.anything(),
    });
  });

  it('acks failed/superseded when the sequence is not newer', () => {
    const state = routerReducer(initialRouterState, {
      type: 'router.commandApplied',
      route: { name: 'dashboard' },
      sequence: 9,
      commandId: 'old',
    });
    const decision = decideCommand(state, command({ command_id: 'cmd_2', sequence: 9 }));
    expect(decision.kind).toBe('reject');
    if (decision.kind === 'reject') {
      expect(decision.ack.status).toBe('failed');
      expect(decision.ack.error_code).toBe('superseded');
    }
  });

  it('ignores a duplicate command id', () => {
    const state = routerReducer(initialRouterState, {
      type: 'router.commandApplied',
      route: { name: 'dashboard' },
      sequence: 1,
      commandId: 'cmd_1',
    });
    expect(decideCommand(state, command({ sequence: 5 })).kind).toBe('ignore');
  });

  it('rejects an unknown collection and a bad photo id', () => {
    const bad = decideCommand(initialRouterState, command({ payload: { route: 'photos', collection: 'secret' } }));
    expect(bad.kind === 'reject' && bad.ack.error_code).toBe('invalid_route');
    const worse = decideCommand(
      initialRouterState,
      command({ kind: 'show', payload: { photo_id: '../etc/passwd' } }),
    );
    expect(worse.kind === 'reject' && worse.ack.error_code).toBe('invalid_payload');
  });

  it('carries the resource id for show', () => {
    const decision = decideCommand(initialRouterState, command({ kind: 'show', payload: { photo_id: 'ph_01' } }));
    expect(decision.kind === 'apply' && decision.resourceId).toBe('ph_01');
  });

  it('treats refresh as a data-only command', () => {
    expect(decideCommand(initialRouterState, command({ kind: 'refresh', payload: {} })).kind).toBe('refresh');
  });
});

describe('render-confirmed acks (design §7.2)', () => {
  const show = command({ command_id: 'cmd_show', kind: 'show', payload: { photo_id: 'ph_07' } });

  it('holds the ack for a show until the image reports onload', () => {
    const decision = decideCommand(initialRouterState, show);
    expect(decision.kind).toBe('apply');
    const pending = pendingAckFor(decision);
    expect(pending?.resourceId).toBe('ph_07');
    expect(resolvePendingAck(pending, null)).toBeNull();
    expect(resolvePendingAck(pending, 'ph_99')).toBeNull();
    const ack = resolvePendingAck(pending, 'ph_07');
    expect(ack).toEqual({
      command_id: 'cmd_show',
      status: 'applied',
      route: { name: 'photo', photo_id: 'ph_07' },
      resource_id: 'ph_07',
    });
  });

  it('acks a navigate on the route change, with nothing to hold back', () => {
    const decision = decideCommand(initialRouterState, command({ payload: { route: 'photos' } }));
    expect(pendingAckFor(decision)).toBeNull();
    expect(needsRenderConfirmation({ name: 'photos', collection: 'recent' })).toBe(false);
  });

  it('fails a held ack when the photo turns out to be unavailable', () => {
    const pending = pendingAckFor(decideCommand(initialRouterState, show));
    expect(pending).not.toBeNull();
    expect(failedAck(pending!)).toEqual({
      command_id: 'cmd_show',
      status: 'failed',
      route: { name: 'photo', photo_id: 'ph_07' },
      resource_id: 'ph_07',
      error_code: 'photo_unavailable',
    });
  });
});
