import { render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';

import { AppContext } from '../app/context';
import { createInitialState } from '../app/state';
import { ApiClient } from '../core/api';
import type { PhotoItem } from '../types/api';
import { PhotoPane } from './PhotoPane';
import { useSlideshow } from './useSlideshow';

// Control asynchronous image loading at the hook boundary to exercise captions.
vi.mock('./useSlideshow', () => ({ useSlideshow: vi.fn() }));

function renderPane(item: PhotoItem | null): void {
  vi.mocked(useSlideshow).mockReturnValue({
    status: item ? 'playing' : 'empty', item, previous: null, fixed: false,
    shown: item ? { id: item.id, src: item.urls.preview, element: new Image() } : null,
    count: item ? 1 : 0,
  });
  render(
    <AppContext.Provider value={{ overview: { load: async () => {}, invalidate: () => {} }, house: { load: async () => {}, invalidate: () => {} }, state: createInitialState(0), api: new ApiClient(),
      dispatch: () => {}, goBack: () => {}, clientVersion: 'test' }}>
      <PhotoPane payload={null} />
    </AppContext.Provider>,
  );
}

describe('dashboard photo captions', () => {
  it('labels an existing photo with missing capture time instead of hiding its caption', () => {
    renderPane({
      id: 'photo', source_id: 'family', captured_at: null, captured_confidence: 'unknown',
      first_seen_at: '2026-09-06T00:00:00Z', is_baseline: false, width: 100, height: 100,
      preview: { status: 'ready', width: 100, height: 100 },
      urls: { preview: '/preview', thumb: '/thumb' },
    });
    expect(screen.getByText('拍摄时间未知')).toBeDefined();
    expect(screen.queryByText(/2026年9月6日/)).toBeNull();
  });

  it('does not show a capture caption before a photo exists', () => {
    renderPane(null);
    expect(screen.queryByText('拍摄时间未知')).toBeNull();
  });
});
