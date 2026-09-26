import { describe, expect, it } from 'vitest';

import { initialPhotoListState } from '../../app/photoList';
import type { PhotoListState } from '../../app/photoList';
import { initialPhotoViewerState } from '../../app/photoViewer';
import type { PhotoViewerState } from '../../app/photoViewer';
import type { PhotoItem } from '../../types/api';
import { filmstripIds, viewerPosition } from './viewerPosition';

const ids = ['a', 'b', 'c', 'd', 'e', 'f', 'g'];
const viewer = (patch: Partial<PhotoViewerState>): PhotoViewerState => ({ ...initialPhotoViewerState, sequence: ids, photoId: 'c', collection: 'recent', ...patch });
const list = (patch: Partial<PhotoListState>): PhotoListState => ({
  ...initialPhotoListState, collection: 'recent', status: 'ready',
  items: ids.map(id => ({ id }) as PhotoItem), ...patch,
});

describe('viewerPosition', () => {
  it('counts against a fully loaded list', () => {
    expect(viewerPosition(viewer({}), list({ cursor: null }))).toBe('最近新增 · 第 3 / 7 张');
  });
  it('uses a server total while more pages exist, else only the position', () => {
    expect(viewerPosition(viewer({}), list({ cursor: 'next', meta: { total: 128 } }))).toBe('最近新增 · 第 3 / 128 张');
    expect(viewerPosition(viewer({}), list({ cursor: 'next' }))).toBe('最近新增 · 第 3 张');
  });
  it('omits the collection for a dashboard sequence and says nothing for a lone photo', () => {
    expect(viewerPosition(viewer({ collection: null }), list({ cursor: null }))).toBe('第 3 张');
    expect(viewerPosition(viewer({ sequence: ['c'] }), list({}))).toBeNull();
  });
});

describe('filmstripIds', () => {
  it('shows neighbours around the current photo and skips failed ones', () => {
    expect(filmstripIds(viewer({}), 2)).toEqual(['a', 'b', 'c', 'd', 'e']);
    expect(filmstripIds(viewer({ failedIds: ['d'] }), 2)).toEqual(['a', 'b', 'c', 'e', 'f']);
    expect(filmstripIds(viewer({ sequence: ['c'] }), 2)).toEqual([]);
  });
});
