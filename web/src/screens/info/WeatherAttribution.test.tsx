import { createRef } from 'react';
import { fireEvent, render, screen } from '@testing-library/react';
import { expect, it, vi } from 'vitest';
import { WeatherAttribution } from './WeatherAttribution';

it('clearly attributes QWeather outside a refresh button and deduplicates the official source link', () => {
  const refresh = vi.fn();
  render(<div><button onClick={refresh}>刷新天气</button><WeatherAttribution provider="qweather" attributions={['https://www.qweather.com/', '示例气象机构', 'https://example.test/license']} /></div>);
  const official = screen.getByRole('link', { name: '和风天气 · QWeather' });
  expect(official.getAttribute('href')).toBe('https://www.qweather.com');
  expect(official.closest('button')).toBeNull();
  expect(official.getAttribute('tabindex')).toBe('-1');
  expect(screen.getAllByRole('link')).toHaveLength(2);
  expect(screen.getByText('示例气象机构')).toBeDefined();
  fireEvent.click(official);
  expect(refresh).not.toHaveBeenCalled();
});

it('renders attribution statements as text and only links credential-free HTTPS URLs', () => {
  render(<WeatherAttribution provider="qweather" attributions={['<img src=x onerror=alert(1)>', 'javascript:alert(1)', 'https://user:secret@example.test/license', 'http://example.test/license', 'https://example.test/license']} />);
  const region = screen.getByRole('region', { name: '天气数据来源' });
  expect(region.querySelector('img')).toBeNull();
  expect(screen.getByText('<img src=x onerror=alert(1)>')).toBeDefined();
  expect(Array.from(region.querySelectorAll('a')).map(link => link.getAttribute('href'))).toEqual(['https://www.qweather.com', 'https://example.test/license']);
});

it('keeps long source statements reachable through remote scrolling and returns upward at the top', () => {
  const ref = createRef<HTMLDivElement>(), top = vi.fn();
  const statements = Array.from({ length: 4 }, (_, index) => `示例来源 ${index}：${'长声明'.repeat(70)}`);
  const { rerender } = render(<WeatherAttribution provider="qweather" attributions={statements} ref={ref} onTop={top} />);
  const region = screen.getByRole('region', { name: '天气数据来源' });
  Object.defineProperties(region, { scrollHeight: { configurable: true, value: 400 }, clientHeight: { configurable: true, value: 40 } });
  fireEvent(window, new Event('resize'));
  region.focus(); fireEvent.keyDown(region, { key: 'ArrowDown' });
  expect(region.scrollTop).toBeGreaterThan(0);
  fireEvent.keyDown(region, { key: 'ArrowUp' });
  expect(region.scrollTop).toBe(0); expect(top).not.toHaveBeenCalled();
  fireEvent.keyDown(region, { key: 'ArrowUp' }); expect(top).toHaveBeenCalledTimes(1);
  statements.forEach(statement => expect(screen.getByText(statement)).toBeDefined());
  rerender(<WeatherAttribution provider="open-meteo" attributions={statements} ref={ref} onTop={top} />);
  expect(screen.queryByRole('region', { name: '天气数据来源' })).toBeNull();
});

it('does not attribute an older Open-Meteo snapshot to QWeather', () => {
  render(<WeatherAttribution provider="open-meteo" />);
  expect(screen.queryByRole('link')).toBeNull();
});

it('returns the remote focus when a refreshed snapshot removes its attribution region', () => {
  const button = createRef<HTMLButtonElement>();
  const contents = (provider: string) => <><button ref={button}>今日天气</button><WeatherAttribution provider={provider} onTop={() => button.current?.focus()} /></>;
  const view = render(contents('qweather'));
  screen.getByRole('region', { name: '天气数据来源' }).focus();
  view.rerender(contents('open-meteo'));
  expect(document.activeElement).toBe(button.current);
});

it('lets a parent reading region scroll complete source text without trapping the remote', () => {
  const parent = vi.fn();
  render(<div onKeyDown={parent}><WeatherAttribution provider="qweather" readingMode="parent" attributions={['示例声明']} /></div>);
  const region = screen.getByRole('region', { name: '天气数据来源' });
  fireEvent.keyDown(region, { key: 'ArrowDown' });
  expect(parent).toHaveBeenCalledTimes(1);
});
