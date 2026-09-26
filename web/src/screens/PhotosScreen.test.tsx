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
import { saveLastAuthOkAt } from '../core/authExpiry';
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
          item: items.find(item => input.split('?')[0]?.endsWith(`/${item.id}`)) ?? items[0],
          neighbors: { previous_id: null, next_id: items[1]?.id ?? null },
        };
      } else if (input.startsWith('/api/v1/videos')) {
        body = { items: [], next_cursor: null };
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
  saveLastAuthOkAt(Date.now());
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
    // Down moves a whole row (4 columns), and never past the last item.
    fireEvent.keyDown(document.activeElement as HTMLElement, { key: 'ArrowDown' });
    await waitFor(() =>
      expect(document.activeElement).toBe(screen.getAllByRole('button').filter(el => el.classList.contains('thumb'))[5] as HTMLElement),
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
    await waitFor(() => expect(screen.getByText(/正在准备照片/)).toBeDefined());

    const image = document.querySelector('.viewer__image') as HTMLImageElement;
    expect(image.getAttribute('src')).toContain('/api/v1/media/photos/ph_0');
    fireEvent.load(image);
    await waitFor(() => expect(screen.queryByText(/正在准备照片/)).toBeNull());
  });

  it('explains an empty recent collection and offers all photos and status', async () => {
    stubApi([]);
    renderAt({ name: 'photos', collection: 'recent' });
    await waitFor(() => expect(screen.getByText(/暂无首次导入后新增的可展示照片/)).toBeDefined());
    expect(screen.getByRole('button', { name: '查看全部照片' })).toBeDefined();
    fireEvent.click(screen.getByRole('button', { name: '查看状态' }));
    await waitFor(() => expect(screen.getByRole('heading', { level: 1, name: /设置/ })).toBeDefined());
  });
  it('focuses tabs without applying, then Enter applies once and focuses the first loaded photo', async () => {
    stubApi(Array.from({ length: 8 }, (_, i) => photo(i)));
    renderAt({ name: 'photos', collection: 'recent' });
    await waitFor(() => expect(document.activeElement).toBe(screen.getByRole('tab', { name: '最近新增' })));
    const recent = screen.getByRole('tab', { name: '最近新增' });
    expect(document.activeElement).toBe(recent);
    fireEvent.keyDown(recent, { key: 'ArrowDown' });
    const today = screen.getByRole('tab', { name: '今天拍摄' });
    expect(document.activeElement).toBe(today);
    expect(recent.getAttribute('aria-selected')).toBe('true');
    expect(today.getAttribute('aria-selected')).toBe('false');
    fireEvent.keyDown(today, { key: 'OK' });
    await waitFor(() => expect(today.getAttribute('aria-selected')).toBe('true'));
    await waitFor(() => expect(document.activeElement).toBe(document.querySelector('.thumb')));
    fireEvent.keyDown(document.activeElement!, { key: 'ArrowLeft' });
    expect(document.activeElement).toBe(today);
    fireEvent.keyDown(today, { key: 'ArrowDown' });
    expect(screen.getByRole('tab', { name: '随心看看' })).toBe(document.activeElement);
    expect(today.getAttribute('aria-selected')).toBe('true');
  });

  it('navigates primary links only on activation and reports a real settings page', async () => {
    stubApi([photo(0)]);
    renderAt({ name: 'photos', collection: 'all' });
    await waitFor(() => expect(document.querySelector('.thumb')).not.toBeNull());
    const settings = screen.getByRole('button', { name: '设置' });
    settings.focus();
    expect(screen.queryByRole('heading', { level: 1, name: /设置/ })).toBeNull();
    fireEvent.keyDown(settings, { key: 'Select' });
    await waitFor(() => expect(screen.getByRole('heading', { level: 1, name: /设置/ })).toBeDefined());
    fireEvent.click(screen.getByRole('tab', { name: '关于 Atrium' }));
    expect(screen.getByText('客户端版本')).toBeDefined();
    expect(screen.getByText('0.0.0+test')).toBeDefined();
    fireEvent.keyDown(document.activeElement!, { key: 'Escape' });
    await waitFor(() => expect(screen.getByRole('tab', { name: '全部照片' })).toBeDefined());
    expect(document.activeElement).toBe(screen.getByRole('button', { name: '设置' }));
  });

  it('keeps old collection photos unavailable during a delayed switch and rapid confirmation', async () => {
    stubApi([photo(0)]);
    const originalFetch = globalThis.fetch;
    let finish!: (response: Response) => void;
    vi.stubGlobal('fetch', vi.fn((input: string) => input.includes('collection=captured_today')
      ? new Promise<Response>(resolve => { finish = resolve; }) : originalFetch(input)));
    renderAt({ name: 'photos', collection: 'recent' });
    await waitFor(() => expect(document.activeElement).toBe(screen.getByRole('tab', { name: '最近新增' })));
    fireEvent.keyDown(document.activeElement!, { key: 'ArrowDown' });
    fireEvent.keyDown(document.activeElement!, { key: 'Enter' });
    await waitFor(() => expect(finish).toBeDefined());
    expect(document.querySelector('.thumb')).toBeNull();
    fireEvent.keyDown(document.activeElement!, { key: 'Enter' });
    expect(document.querySelector('.viewer')).toBeNull();
    finish(new Response(JSON.stringify({ items: [photo(7)], next_cursor: null, meta: {} }), { status: 200 }));
    await waitFor(() => expect(document.activeElement).toBe(document.querySelector('.thumb')));
    expect(document.querySelector('.thumb img')?.getAttribute('src')).toBe('/t/ph_7');
  });

  it('enters a collection from the shared rail on the video page and waits for confirmation', async () => {
    stubApi([photo(0)]);
    renderAt({ name: 'videos' });
    const rail = await screen.findByRole('tab', { name: '视频' });
    expect(rail.getAttribute('aria-selected')).toBe('true');
    fireEvent.click(screen.getByRole('tab', { name: '最近新增' }));
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
    fireEvent.keyDown(all, left);
    expect(document.activeElement).toBe(tab);
    fireEvent.keyDown(tab, { key: 'Enter' });
    fireEvent.keyDown(document.activeElement!, down);
    expect(document.activeElement).toBe(all);
    fireEvent.keyDown(all, up);
    expect(document.activeElement).toBe(screen.getByRole('button', { name: '影像' }));
  });

});

it('returns from operations to the same gallery card and scroll without repeated Back leaking', async () => {
  stubApi(Array.from({ length: 12 }, (_, i) => photo(i)));
  renderAt({ name: 'photos', collection: 'random' });
  await waitFor(() => expect(document.querySelectorAll('.thumb')).toHaveLength(12));
  const card = document.querySelectorAll<HTMLElement>('.thumb')[7]!;
  const grid = document.querySelector('.photogrid')!;
  grid.scrollTop = 245;
  fireEvent.click(card);
  await waitFor(() => expect(document.querySelector('.viewer__image')).not.toBeNull());
  fireEvent.keyDown(document.activeElement!, { key: 'Enter' });
  expect(document.activeElement).toBe(screen.getByRole('button', { name: '关闭操作' }));
  fireEvent.keyDown(document.activeElement!, { key: 'Escape', repeat: true });
  expect(screen.getByRole('button', { name: '关闭操作' })).toBeDefined();
  fireEvent.keyDown(document.activeElement!, { key: 'Escape' });
  expect(screen.queryByRole('button', { name: '关闭操作' })).toBeNull();
  expect(document.querySelector('.viewer')).not.toBeNull();
  fireEvent.keyDown(document.activeElement!, { key: 'Escape' });
  await waitFor(() => expect(document.activeElement).toBe(document.querySelectorAll('.thumb')[7]));
  expect(document.querySelector('.photogrid')?.scrollTop).toBe(245);
  expect(screen.getByRole('tab', { name: '随心看看' }).getAttribute('aria-selected')).toBe('true');
});

it('uses the responsive column count for both the grid and short-tail navigation', async () => {
  vi.stubGlobal('innerWidth', 804);
  stubApi(Array.from({ length: 5 }, (_, i) => photo(i)));
  renderAt({ name: 'photos', collection: 'recent' });
  await waitFor(() => expect(document.querySelectorAll('.thumb')).toHaveLength(5));
  fireEvent.keyDown(document.activeElement!, { key: 'Enter' });
  fireEvent.keyDown(document.activeElement!, { key: 'ArrowRight' });
  fireEvent.keyDown(document.activeElement!, { key: 'ArrowRight' });
  const shortTailColumn = document.activeElement;
  fireEvent.keyDown(document.activeElement!, { key: 'ArrowDown' });
  expect(document.activeElement).toBe(shortTailColumn);
  expect((document.querySelector('.screen--photos') as HTMLElement).style.getPropertyValue('--photo-columns')).toBe('3');
  fireEvent.keyDown(document.activeElement!, { key: 'ArrowLeft' });
  fireEvent.keyDown(document.activeElement!, { key: 'ArrowDown' });
  expect(document.activeElement).toBe(document.querySelectorAll('.thumb')[4]);
  fireEvent.keyDown(document.activeElement!, { key: 'ArrowDown' });
  expect(document.activeElement).toBe(document.querySelectorAll('.thumb')[4]);
  vi.stubGlobal('innerWidth', 1920);
  fireEvent(window, new Event('resize'));
  expect((document.querySelector('.screen--photos') as HTMLElement).style.getPropertyValue('--photo-columns')).toBe('4');
});

it('preserves the paginated entry card while browsing several frozen neighbors', async () => {
  const items = Array.from({ length: 22 }, (_, i) => photo(i));
  stubApi(items);
  const original = globalThis.fetch;
  const fetcher = vi.fn((input: string) => input.startsWith('/api/v1/photos?')
    ? Promise.resolve(new Response(JSON.stringify({ items: input.includes('cursor=page2') ? items.slice(12) : items.slice(0, 12), next_cursor: input.includes('cursor=page2') ? null : 'page2' }), { status: 200 })) : original(input));
  vi.stubGlobal('fetch', fetcher);
  renderAt({ name: 'photos', collection: 'random' });
  await waitFor(() => expect(document.querySelectorAll('.thumb')).toHaveLength(12));
  fireEvent.focus(document.querySelectorAll('.thumb')[8]!);
  await waitFor(() => expect(document.querySelectorAll('.thumb')).toHaveLength(22));
  document.querySelector('.photogrid')!.scrollTop = 420;
  fireEvent.click(document.querySelectorAll('.thumb')[15]!);
  await waitFor(() => expect(document.querySelector('.viewer__image')?.getAttribute('src')).toContain('ph_15'));
  for (const id of [16, 17, 18]) {
    fireEvent.keyDown(document.activeElement!, { key: 'ArrowRight' });
    await waitFor(() => expect(document.querySelector('.viewer__image:not(.viewer__image--retained)')?.getAttribute('src')).toContain(`ph_${id}`));
  }
  expect(fetcher.mock.calls.filter(([url]) => url.startsWith('/api/v1/photos?'))).toHaveLength(2);
  expect(fetcher.mock.calls.filter(([url]) => url.startsWith('/api/v1/photos/') && url.includes('collection='))).toHaveLength(0);
  fireEvent.keyDown(document.activeElement!, { key: 'Escape' });
  await waitFor(() => expect(document.activeElement).toBe(document.querySelectorAll('.thumb')[15]));
  expect(document.querySelector('.photogrid')?.scrollTop).toBe(420);
});

it('retains a rendered image on failure and only skips on an explicit direction', async () => {
  stubApi(Array.from({ length: 3 }, (_, i) => photo(i)));
  renderAt({ name: 'photos', collection: 'all' });
  await waitFor(() => expect(document.querySelectorAll('.thumb')).toHaveLength(3));
  fireEvent.click(document.querySelector('.thumb')!);
  await waitFor(() => expect(document.querySelector('.viewer__image')).not.toBeNull());
  fireEvent.load(document.querySelector('.viewer__image')!);
  fireEvent.keyDown(document.activeElement!, { key: 'ArrowRight' });
  await waitFor(() => expect(document.querySelector('.viewer__image:not(.viewer__image--retained)')?.getAttribute('src')).toContain('ph_1'));
  fireEvent.error(document.querySelector('.viewer__image:not(.viewer__image--retained)')!);
  await screen.findByText(/这张照片暂时无法显示/);
  expect(document.querySelector('.viewer__image--retained')?.getAttribute('src')).toContain('ph_0');
  fireEvent.keyDown(document.activeElement!, { key: 'ArrowRight' });
  await waitFor(() => expect(document.querySelector('.viewer__image:not(.viewer__image--retained)')?.getAttribute('src')).toContain('ph_2'));
  fireEvent.load(document.querySelector('.viewer__image:not(.viewer__image--retained)')!);
  expect(document.querySelector('.viewer__image--retained')).toBeNull();
  fireEvent.keyDown(document.activeElement!, { key: 'ArrowLeft' });
  await waitFor(() => expect(document.querySelector('.viewer__image:not(.viewer__image--retained)')?.getAttribute('src')).toContain('ph_0'));
});

it('groups the capture-ordered collection by month and leaves the grid through its edges', async () => {
  const items = [1, 2, 3, 4, 5].map(i => ({ ...photo(i), captured_at: i <= 2 ? `2026-09-0${i}T10:00:00+08:00` : `2026-08-2${i}T10:00:00+08:00` }));
  stubApi([...items, photo(0)]);
  renderAt({ name: 'photos', collection: 'all' });
  await waitFor(() => expect(document.querySelectorAll('.thumb')).toHaveLength(6));
  expect(Array.from(document.querySelectorAll('.media-grid__month')).map(el => el.textContent)).toEqual(['2026年9月', '2026年8月', '拍摄时间未知']);
  fireEvent.keyDown(document.activeElement!, { key: 'ArrowRight' });
  const thumbs = () => document.querySelectorAll<HTMLElement>('.thumb');
  await waitFor(() => expect(document.activeElement).toBe(thumbs()[0]));
  // Down from September's second tile enters August at the same column.
  fireEvent.keyDown(document.activeElement!, { key: 'ArrowRight' });
  fireEvent.keyDown(document.activeElement!, { key: 'ArrowDown' });
  await waitFor(() => expect(document.activeElement).toBe(thumbs()[3]));
  // Down again crosses into the one-photo section and clamps to it.
  fireEvent.keyDown(document.activeElement!, { key: 'ArrowDown' });
  await waitFor(() => expect(document.activeElement).toBe(thumbs()[5]));
  fireEvent.keyDown(document.activeElement!, { key: 'ArrowLeft' });
  expect(document.activeElement).toBe(screen.getByRole('tab', { name: '全部照片' }));
  fireEvent.keyDown(document.activeElement!, { key: 'ArrowRight' });
  await waitFor(() => expect(document.activeElement).toBe(thumbs()[5]));
  fireEvent.keyDown(document.activeElement!, { key: 'ArrowUp' });
  fireEvent.keyDown(document.activeElement!, { key: 'ArrowUp' });
  await waitFor(() => expect(document.activeElement).toBe(thumbs()[0]));
  fireEvent.keyDown(document.activeElement!, { key: 'ArrowUp' });
  const media = screen.getByRole('button', { name: '影像' });
  expect(document.activeElement).toBe(media);
  expect(media.getAttribute('aria-current')).toBe('page');
  fireEvent.keyDown(media, { key: 'ArrowDown' });
  expect(document.activeElement).toBe(screen.getByRole('tab', { name: '全部照片' }));
});

it('keeps recent and random ungrouped and reaches the top navigation from the first rail entry', async () => {
  stubApi(Array.from({ length: 3 }, (_, i) => photo(i)));
  renderAt({ name: 'photos', collection: 'recent' });
  await waitFor(() => expect(document.querySelectorAll('.thumb')).toHaveLength(3));
  expect(document.querySelector('.media-grid__month')).toBeNull();
  fireEvent.keyDown(document.activeElement!, { key: 'ArrowUp' });
  expect(document.activeElement).toBe(screen.getByRole('button', { name: '影像' }));
});

it('opens videos from the rail without adding a return layer', async () => {
  stubApi([photo(0)]);
  renderAt({ name: 'photos', collection: 'recent' });
  const videos = await screen.findByRole('tab', { name: '视频' });
  fireEvent.click(videos);
  await waitFor(() => expect(screen.getByRole('tab', { name: '视频' }).getAttribute('aria-selected')).toBe('true'));
  expect(screen.getByRole('region', { name: '视频列表' })).toBeDefined();
  fireEvent.keyDown(document.body, { key: 'Escape' });
  await waitFor(() => expect(document.querySelector('.library')).toBeNull());
});
