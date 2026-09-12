/**
 * Collection browser and viewer, rendered for real (W-202, W-203): remote keys
 * move the focus, Enter opens the photo, and an empty `recent` explains that
 * the baseline import is not "new".
 */

import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import type { ReactElement } from 'react';
import { useEffect } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { CurrentScreen } from '../App';
import { AppProvider } from '../app/AppProvider';
import { useApp } from '../app/context';
import type { AppRoute } from '../app/router';
import type { HomeResponse, PhotoItem } from '../types/api';

const HOME: HomeResponse = {
  schema_version: 1,
  server_time: new Date().toISOString(),
  version: 1,
  home: { name: 'Demo Home', timezone: 'Asia/Singapore' },
  widgets: [
    {
      type: 'clock',
      payload: {
        timezone: 'Asia/Singapore',
        server_time: new Date().toISOString(),
        utc_offset_seconds: 28800,
      },
    },
  ],
};

function photo(index: number): PhotoItem {
  const id = `ph_${index}`;
  return {
    id,
    source_id: 'family_photos',
    captured_at: index === 0 ? null : `2026-09-0${(index % 5) + 1}T10:15:00+08:00`,
    captured_confidence: index === 0 ? 'unknown' : index === 1 ? 'inferred' : 'exact',
    first_seen_at: '2026-09-05T02:00:00Z',
    is_baseline: false,
    width: 1600,
    height: 1200,
    preview: { status: 'ready', width: 1280, height: 960 },
    urls: { preview: `/api/v1/media/photos/${id}?variant=preview`, thumb: `/t/${id}` },
  };
}

class StubSocket {
  onopen: ((e: unknown) => void) | null = null;
  onclose: ((e: unknown) => void) | null = null;
  onerror: ((e: unknown) => void) | null = null;
  onmessage: ((e: unknown) => void) | null = null;
  send(): void {}
  close(): void {}
}

function stubApi(items: PhotoItem[]): void {
  vi.stubGlobal('WebSocket', StubSocket);
  vi.stubGlobal(
    'fetch',
    vi.fn((input: string) => {
      let body: unknown = HOME;
      if (input.startsWith('/api/v1/photos?')) {
        body = { items, next_cursor: null, meta: { unknown_captured_count: 2 } };
      } else if (input.startsWith('/api/v1/photos/')) {
        body = {
          item: items[0],
          neighbors: { previous_id: null, next_id: items[1]?.id ?? null },
        };
      } else if (input.startsWith('/api/v1/media/')) {
        return Promise.resolve(new Response('', { status: 200 }));
      }
      return Promise.resolve(
        new Response(JSON.stringify(body), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      );
    }),
  );
}

function Harness(props: { route: AppRoute }): ReactElement {
  const { dispatch } = useApp();
  useEffect(() => {
    dispatch({ type: 'router.navigate', route: props.route });
  }, [dispatch, props.route]);
  return <CurrentScreen />;
}

function renderAt(route: AppRoute): void {
  render(
    <AppProvider>
      <Harness route={route} />
    </AppProvider>,
  );
}

beforeEach(() => {
  vi.unstubAllGlobals();
});

describe('collection browser (W-202)', () => {
  it('renders thumbs with captions and moves the focus with the remote', async () => {
    stubApi(Array.from({ length: 8 }, (_, i) => photo(i)));
    renderAt({ name: 'photos', collection: 'captured_today' });

    await waitFor(() => expect(screen.getAllByRole('button').filter(el => el.classList.contains('thumb'))).toHaveLength(8));
    expect(screen.getByRole('tab', { name: '今天拍摄' })).toBeDefined();
    // captured_today carries the "not classified" count from `meta`.
    expect(screen.getByText(/2 张照片缺少拍摄时间/)).toBeDefined();
    expect(screen.getByText('拍摄时间未知')).toBeDefined();
    expect(screen.getAllByText('估算').length).toBeGreaterThan(0);

    const first = screen.getAllByRole('button').filter(el => el.classList.contains('thumb'))[0] as HTMLElement;
    expect(document.activeElement).toBe(screen.getByRole('tab', { name: '今天拍摄' }));
    fireEvent.keyDown(document.activeElement!, { key: 'Enter' });
    await waitFor(() => expect(document.activeElement).toBe(first));
    fireEvent.keyDown(first, { key: 'ArrowRight' });
    await waitFor(() =>
      expect(document.activeElement).toBe(screen.getAllByRole('button').filter(el => el.classList.contains('thumb'))[1] as HTMLElement),
    );
    // Down moves a whole row (5 columns), and never past the last item.
    fireEvent.keyDown(document.activeElement as HTMLElement, { key: 'ArrowDown' });
    await waitFor(() =>
      expect(document.activeElement).toBe(screen.getAllByRole('button').filter(el => el.classList.contains('thumb'))[6] as HTMLElement),
    );
  });

  it('opens the viewer on Enter and reports the render signal', async () => {
    stubApi(Array.from({ length: 3 }, (_, i) => photo(i)));
    renderAt({ name: 'photos', collection: 'recent' });

    await waitFor(() => expect(screen.getAllByRole('button').filter(el => el.classList.contains('thumb'))).toHaveLength(3));
    expect(document.activeElement).toBe(screen.getByRole('tab', { name: '最近新增' }));
    fireEvent.keyDown(document.activeElement!, { key: 'Enter' });
    await waitFor(() => expect(document.activeElement?.classList.contains('thumb')).toBe(true));
    fireEvent.keyDown(document.activeElement!, { key: 'Enter' });
    await waitFor(() => expect(screen.getByText(/Preparing this photo/)).toBeDefined());

    const image = document.querySelector('.viewer__image') as HTMLImageElement;
    expect(image.getAttribute('src')).toContain('/api/v1/media/photos/ph_0');
    fireEvent.load(image);
    await waitFor(() => expect(screen.queryByText(/Preparing this photo/)).toBeNull());
  });

  it('explains an empty recent collection and offers all photos and status', async () => {
    stubApi([]);
    renderAt({ name: 'photos', collection: 'recent' });
    await waitFor(() => expect(screen.getByText(/暂无首次导入后新增的可展示照片/)).toBeDefined());
    expect(screen.getByRole('button', { name: '查看全部照片' })).toBeDefined();
    fireEvent.click(screen.getByRole('button', { name: '查看状态' }));
    await waitFor(() => expect(screen.getByRole('heading', { name: '设置与状态' })).toBeDefined());
  });
  it('focuses tabs without applying, then Enter applies once and focuses the first loaded photo', async () => {
    stubApi(Array.from({ length: 8 }, (_, i) => photo(i)));
    renderAt({ name: 'photos', collection: 'recent' });
    await waitFor(() => expect(document.activeElement).toBe(screen.getByRole('tab', { name: '最近新增' })));
    const recent = screen.getByRole('tab', { name: '最近新增' });
    expect(document.activeElement).toBe(recent);
    fireEvent.keyDown(recent, { key: 'ArrowRight' });
    const today = screen.getByRole('tab', { name: '今天拍摄' });
    expect(document.activeElement).toBe(today);
    expect(recent.getAttribute('aria-selected')).toBe('true');
    expect(today.getAttribute('aria-selected')).toBe('false');
    fireEvent.keyDown(today, { key: 'OK' });
    await waitFor(() => expect(today.getAttribute('aria-selected')).toBe('true'));
    await waitFor(() => expect(document.activeElement).toBe(document.querySelector('.thumb')));
    fireEvent.keyDown(document.activeElement!, { key: 'ArrowUp' });
    fireEvent.keyDown(today, { key: 'ArrowRight' });
    expect(screen.getByRole('tab', { name: '随心看看' })).toBe(document.activeElement);
    expect(today.getAttribute('aria-selected')).toBe('true');
  });

  it('navigates primary links only on activation and reports a real settings page', async () => {
    stubApi([photo(0)]);
    renderAt({ name: 'photos', collection: 'all' });
    await waitFor(() => expect(document.querySelector('.thumb')).not.toBeNull());
    const settings = screen.getByRole('button', { name: '设置' });
    settings.focus();
    expect(screen.queryByRole('heading', { name: '设置与状态' })).toBeNull();
    fireEvent.keyDown(settings, { key: 'Select' });
    await waitFor(() => expect(screen.getByRole('heading', { name: '设置与状态' })).toBeDefined());
    expect(screen.getByText('客户端版本')).toBeDefined();
    expect(screen.getByText('0.0.0+test')).toBeDefined();
    fireEvent.keyDown(document.activeElement!, { key: 'Escape' });
    await waitFor(() => expect(screen.getByRole('button', { name: '打开当前照片' })).toBeDefined());
  });

  it('keeps old collection photos unavailable during a delayed switch and rapid confirmation', async () => {
    stubApi([photo(0)]);
    const originalFetch = globalThis.fetch;
    let finish!: (response: Response) => void;
    vi.stubGlobal('fetch', vi.fn((input: string) => input.includes('collection=captured_today')
      ? new Promise<Response>(resolve => { finish = resolve; }) : originalFetch(input)));
    renderAt({ name: 'photos', collection: 'recent' });
    await waitFor(() => expect(document.activeElement).toBe(screen.getByRole('tab', { name: '最近新增' })));
    fireEvent.keyDown(document.activeElement!, { key: 'ArrowRight' });
    fireEvent.keyDown(document.activeElement!, { key: 'Enter' });
    await waitFor(() => expect(finish).toBeDefined());
    expect(document.querySelector('.thumb')).toBeNull();
    fireEvent.keyDown(document.activeElement!, { key: 'Enter' });
    expect(document.querySelector('.viewer')).toBeNull();
    finish(new Response(JSON.stringify({ items: [photo(7)], next_cursor: null, meta: {} }), { status: 200 }));
    await waitFor(() => expect(document.activeElement).toBe(document.querySelector('.thumb')));
    expect(document.querySelector('.thumb img')?.getAttribute('src')).toBe('/t/ph_7');
  });

  it('enters the selected collection tab from primary navigation and waits for confirmation', async () => {
    stubApi([photo(0)]);
    renderAt({ name: 'dashboard' });
    await waitFor(() => expect(screen.getByRole('button', { name: '照片' })).toBeDefined());
    fireEvent.click(screen.getByRole('button', { name: '照片' }));
    const recent = await screen.findByRole('tab', { name: '最近新增' });
    expect(document.activeElement).toBe(recent);
    await waitFor(() => expect(document.querySelector('.thumb')).not.toBeNull());
    expect(document.activeElement).toBe(recent);
    fireEvent.keyDown(recent, { key: 'Enter' });
    await waitFor(() => expect(document.activeElement).toBe(document.querySelector('.thumb')));
    expect(document.querySelector('.viewer')).toBeNull();
  });

  it('describes an empty today collection only in terms of displayable photos', async () => {
    stubApi([]);
    renderAt({ name: 'photos', collection: 'captured_today' });
    await waitFor(() => expect(screen.getByText('暂无可展示的今日照片。')).toBeDefined());
    expect(screen.queryByText('今天还没有拍摄的照片。')).toBeNull();
    expect(document.activeElement).toBe(screen.getByRole('tab', { name: '今天拍摄' }));
    fireEvent.keyDown(document.activeElement!, { key: 'Enter' });
    expect(document.activeElement).toBe(screen.getByRole('button', { name: '查看全部照片' }));
  });

  it.each([
    ['recent', '最近新增', '查看全部照片'],
    ['all', '全部照片', '重试'],
  ] as const)('confirms a failed %s collection into recovery without another request', async (collection, label, action) => {
    stubApi([]);
    const originalFetch = globalThis.fetch;
    const requests = vi.fn();
    vi.stubGlobal('fetch', vi.fn((input: string) => {
      if (input.includes(`collection=${collection}`)) {
        requests();
        return Promise.resolve(new Response(JSON.stringify({ error: { code: 'internal_error', message: 'failed' } }), { status: 500 }));
      }
      return originalFetch(input);
    }));
    renderAt({ name: 'photos', collection });
    await screen.findByText('暂时无法加载此合集。');
    expect(document.activeElement).toBe(screen.getByRole('tab', { name: label }));
    expect(requests).toHaveBeenCalledTimes(1);
    fireEvent.keyDown(document.activeElement!, { key: 'Enter' });
    expect(document.activeElement).toBe(screen.getByRole('button', { name: action }));
    expect(requests).toHaveBeenCalledTimes(1);
  });

  it.each([
    ['legacy', { key: 'Right' }, { key: 'Left' }, { key: 'Up' }, { key: 'Down' }],
    ['numeric fallback', { key: 'TVRemote', keyCode: 39 }, { key: 'TVRemote', keyCode: 37 }, { key: 'TVRemote', keyCode: 38 }, { key: 'TVRemote', keyCode: 40 }],
  ] as const)('moves through recovery with %s direction aliases', async (_name, right, left, up, down) => {
    stubApi([]);
    renderAt({ name: 'photos', collection: 'recent' });
    await screen.findByText('暂无首次导入后新增的可展示照片。');
    const tab = screen.getByRole('tab', { name: '最近新增' });
    const all = screen.getByRole('button', { name: '查看全部照片' });
    fireEvent.keyDown(tab, { key: 'Enter' });
    expect(document.activeElement).toBe(all);
    fireEvent.keyDown(all, right);
    expect(document.activeElement).toBe(screen.getByRole('button', { name: '查看状态' }));
    fireEvent.keyDown(document.activeElement!, left);
    expect(document.activeElement).toBe(all);
    fireEvent.keyDown(all, up);
    expect(document.activeElement).toBe(tab);
    fireEvent.keyDown(tab, { key: 'Enter' });
    fireEvent.keyDown(document.activeElement!, down);
    expect(document.activeElement).toBe(screen.getByRole('button', { name: '照片' }));
  });

});
