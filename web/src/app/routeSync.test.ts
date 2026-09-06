/**
 * W-304: which route survives a reconnect (PRD 5.3, design §7.2).
 */

import { describe, expect, it } from 'vitest';

import {
  coldStartRoute,
  isColdStart,
  photoToRevalidate,
  routeAfterReconnect,
} from './routeSync';
import type { AppRoute } from './router';

describe('cold start (W-304)', () => {
  it('is a session with no snapshot and no applied command', () => {
    expect(isColdStart({ hasSnapshot: false, appliedSequence: 0 })).toBe(true);
    expect(isColdStart({ hasSnapshot: true, appliedSequence: 0 })).toBe(false);
    expect(isColdStart({ hasSnapshot: false, appliedSequence: 4 })).toBe(false);
  });

  it('reports the dashboard', () => {
    expect(coldStartRoute()).toEqual({ name: 'dashboard' });
  });
});

describe('route after reconnect (W-304)', () => {
  const photos: AppRoute = { name: 'photos', collection: 'captured_today' };
  const photo: AppRoute = { name: 'photo', photoId: 'ph_7', collection: 'recent' };

  it('keeps a collection route: an empty collection has its own empty state', () => {
    expect(routeAfterReconnect(photos, 'present')).toBe(photos);
    expect(routeAfterReconnect(photos, 'gone')).toBe(photos);
  });

  it('keeps the dashboard', () => {
    const dashboard: AppRoute = { name: 'dashboard' };
    expect(routeAfterReconnect(dashboard, 'present')).toBe(dashboard);
  });

  it('drops a photo the fresh snapshot no longer serves', () => {
    expect(routeAfterReconnect(photo, 'gone')).toEqual({ name: 'dashboard' });
  });

  it('keeps a photo whose check failed for transport reasons', () => {
    expect(routeAfterReconnect(photo, 'unknown')).toBe(photo);
    expect(routeAfterReconnect(photo, 'present')).toBe(photo);
  });

  it('never restores a transport screen as content', () => {
    expect(routeAfterReconnect({ name: 'pair' }, 'present')).toEqual({ name: 'dashboard' });
    expect(routeAfterReconnect({ name: 'connect' }, 'present')).toEqual({ name: 'dashboard' });
  });

  it('only re-validates a photo route', () => {
    expect(photoToRevalidate(photo)).toBe('ph_7');
    expect(photoToRevalidate(photos)).toBeNull();
    expect(photoToRevalidate({ name: 'dashboard' })).toBeNull();
  });
});
