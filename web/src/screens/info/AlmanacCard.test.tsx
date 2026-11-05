import { fireEvent, render, screen, within } from '@testing-library/react';
import { expect, it, vi } from 'vitest';
import { AlmanacCard } from './AlmanacCard';
import * as presentation from './almanacPresentation';

it('keeps the calendar title simple while preserving all genuine pillars and activities', () => {
  const refresh = vi.fn();
  render(<AlmanacCard day={{ year: 2026, month: 2, day: 4, weekday: '星期三', label: '2月4日 · 星期三' }} onClick={refresh} />);
  const card = screen.getByRole('button', { name: '万年历' });
  expect(within(card).getByText('丙午年')).toBeDefined(); expect(within(card).getByText('庚寅月')).toBeDefined();
  expect(within(card).getByText('己酉日')).toBeDefined(); expect(card.textContent).not.toContain('乙巳蛇年');
  expect(within(card).getByText('万年历')).toBeDefined();
  expect(card.textContent).not.toContain('KOI'); expect(card.textContent).not.toContain('koi-almanac');
  expect(within(card).getByRole('list', { name: '宜' }).textContent).toContain('安床');
  expect(within(card).getByRole('list', { name: '宜' }).textContent).toContain('祭祀');
  expect(within(card).getByRole('list', { name: '忌' }).querySelectorAll('[role="listitem"]')).toHaveLength(3);
  expect(within(card).queryByText('暂无资料')).toBeNull();
  fireEvent.keyDown(card, { key: 'Enter' }); expect(refresh).toHaveBeenCalledTimes(1);
});

it('distinguishes genuinely empty KOI activity arrays from unsupported dates', () => {
  const model = presentation.presentAlmanac({ year: 2026, month: 2, day: 4 });
  vi.spyOn(presentation, 'presentAlmanac').mockReturnValue({ ...model, info: model.info ? { ...model.info, yi: [], ji: [] } : null });
  render(<AlmanacCard day={{ year: 2026, month: 2, day: 4, weekday: '星期三', label: '' }} />);
  expect(screen.getByText('暂无宜')).toBeDefined(); expect(screen.getByText('暂无忌')).toBeDefined();
  expect(screen.queryByText(/超出/)).toBeNull();
});

it('retains the basic Gregorian calendar while explicitly identifying dates outside the KOI table range', () => {
  render(<AlmanacCard day={{ year: 2100, month: 1, day: 1, weekday: '星期五', label: '' }} />);
  expect(screen.getByTestId('almanac-date').textContent).toBe('1');
  expect(screen.getByText('支持 1901—2099')).toBeDefined();
  expect(screen.queryByText('暂无宜')).toBeNull();
  expect(screen.queryByRole('progressbar', { name: '节气进度' })).toBeNull();
});
