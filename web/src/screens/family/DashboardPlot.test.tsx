import { render, screen } from '@testing-library/react';
import { expect, it } from 'vitest';
import { DashboardPlot } from './DashboardPlot';

it('draws a smooth filled milk curve with right-side ticks and only the latest marker', () => {
    const { container } = render(<DashboardPlot label="合成奶量" unit="ml" timezone="Asia/Singapore" period="day" dates={['2026-10-21']} zero points={[
        { date: '2026-10-21T01:00:00+08:00', value: '120' },
        { date: '2026-10-21T06:00:00+08:00', value: '150' },
        { date: '2026-10-21T18:00:00+08:00', value: '140' },
    ]}/>);
    expect(screen.getByRole('img', { name: '合成奶量' })).toBeTruthy();
    expect(container.querySelector('[data-curve]')?.getAttribute('d')).toContain(' C ');
    expect(container.querySelector('[data-area]')?.getAttribute('fill')).toContain('url(#');
    expect(container.querySelectorAll('[data-latest]')).toHaveLength(1);
    const ticks = [...container.querySelectorAll('[data-axis="y"] text')];
    expect(ticks).toHaveLength(3);
    expect(ticks.every(t => Number(t.getAttribute('x')) > 744)).toBe(true);
    expect(container.querySelector('[data-axis="x"]')?.textContent).toBe('0时6时12时18时24时');
    expect(Number(container.querySelector('[data-latest]')?.getAttribute('cx'))).toBeCloseTo(558);
});

it('keeps one real growth observation visible and gives every chart a unique gradient', () => {
    const { container } = render(<><DashboardPlot mini label="合成身高" unit="cm" timezone="UTC" points={[{ date: '2026-10-13', value: '72.6' }]}/><DashboardPlot mini label="合成体重" unit="kg" timezone="UTC" points={[{ date: '2026-10-13', value: '9.3' }]}/></>);
    expect(container.querySelectorAll('[data-latest]')).toHaveLength(2);
    const gradients = [...container.querySelectorAll('linearGradient')].map(el => el.id);
    expect(new Set(gradients).size).toBe(2);
    expect(container.innerHTML).not.toMatch(/NaN|Infinity/);
    expect(container.querySelectorAll('[data-axis="y"]')).toHaveLength(0);
});

it('does not invent a curve when there are no dated numerical observations', () => {
    const { container } = render(<DashboardPlot label="空图" unit="cm" timezone="UTC" points={[{ date: '2026-10', value: '72.6' }, { date: '2026-10-13', value: '' }]}/>);
    expect(container.querySelector('[data-curve]')).toBeNull();
    expect(container.querySelector('[data-latest]')).toBeNull();
});

it('does not assign a time of day to a milk observation with only a calendar date', () => {
    const { container } = render(<DashboardPlot label="日期不含时刻" unit="ml" timezone="Asia/Singapore" period="day" points={[{ date: '2026-10-21', value: '120' }]}/>);
    expect(container.querySelector('[data-latest]')).toBeNull();
});
