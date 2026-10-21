import { fireEvent, render, screen } from '@testing-library/react';
import { expect, it, vi } from 'vitest';
import type { ArchiveAnalysis } from '../../types/archives';
import { GrowthCurve, type GrowthCurveProps } from './GrowthCurve';

const analysis = (groups: ArchiveAnalysis['groups'], complete = true): ArchiveAnalysis => ({ snapshot_id: 'synthetic', data_epoch: 1, generation: 2, projection: 'public', algorithm_version: 'test', groups, coverage: { complete, included: groups.length, excluded: complete ? 0 : 1, reasons: [] } });
const height = analysis([{ metric: 'height', unit: 'cm', value: '70.2', date: '2026-07-13' }, { metric: 'height', unit: 'cm', value: '71.9', date: '2026-08-13' }, { metric: 'height', unit: 'cm', value: '72.6', date: '2026-10-13' }]);
const props = (more: Partial<GrowthCurveProps> = {}): GrowthCurveProps => ({ data: { height: [height], weight: [], limited: [] }, personName: '合成宝宝', timezone: 'Asia/Singapore', busy: false, onUp: vi.fn(), onDown: vi.fn(), onLeft: vi.fn(), onRead: vi.fn(), ...more });

it('shows measured growth across the whole card without inventing age or a WHO comparison', () => {
    const { container } = render(<GrowthCurve {...props()}/>);
    expect(screen.getByRole('heading', { name: '生长曲线 · 合成宝宝' })).toBeTruthy();
    expect(screen.getByRole('img', { name: '生长曲线 · 合成宝宝 · 身高，3 条实测' })).toBeTruthy();
    expect(container.querySelector('.growth-curve__latest')?.textContent).toContain('72.6cm');
    expect(container.querySelector('.growth-curve__caption')?.textContent).toBe('最新测量 10月13日');
    expect(screen.getByText('+0.7 cm')).toBeTruthy();
    expect(screen.getByText('+2.4 cm')).toBeTruthy();
    expect(screen.getByText('近 3 个月已记录增长')).toBeTruthy();
    expect(screen.getByText(/缺少出生日期与性别/)).toBeTruthy();
    expect(container.textContent).not.toMatch(/正常范围|同龄百分位|12 月龄|P30/);
});

it('switches to weight and keeps its records and units separate', () => {
    const { container } = render(<GrowthCurve {...props({ data: { height: [height], weight: [analysis([{ metric: 'weight', unit: 'kg', value: '9.3', date: '2026-10-13' }])], limited: [] } })}/>);
    fireEvent.click(screen.getByRole('button', { name: '体重' }));
    expect(screen.getByRole('button', { name: '体重' }).getAttribute('aria-pressed')).toBe('true');
    expect(screen.getByRole('img', { name: '生长曲线 · 合成宝宝 · 体重，1 条实测' })).toBeTruthy();
    expect(container.querySelector('.growth-curve__latest')?.textContent).toBe('9.3kg');
    expect(screen.queryByText('72.6')).toBeNull();
    expect(screen.getByText('仅有 1 次测量，暂未形成连续曲线')).toBeTruthy();
});

it('keeps distinct units and samples in their own measurement series', () => {
    const { container } = render(<GrowthCurve {...props({ data: { height: [analysis([{ metric: 'height', unit: 'cm', sample: '卧位', value: '72.6', date: '2026-10-13' }, { metric: 'height', unit: 'm', sample: '站立', value: '0.72', date: '2026-10-13' }])], weight: [], limited: [] } })}/>);
    expect(container.querySelectorAll('.growth-curve__series')).toHaveLength(2);
    expect(container.querySelectorAll('[data-measurement]')).toHaveLength(2);
    expect(screen.getByText('卧位 · cm')).toBeTruthy();
    expect(screen.getByText('站立 · m')).toBeTruthy();
});

it('keeps its space and an honest empty state when no person or verified measurements are available', () => {
    const { container, rerender } = render(<GrowthCurve {...props({ personName: null, data: null })}/>);
    expect(container.querySelector('.growth-curve')).toBeTruthy();
    expect(screen.getByText('选择一位家人查看生长曲线')).toBeTruthy();
    expect(container.querySelectorAll('[data-measurement]')).toHaveLength(0);
    rerender(<GrowthCurve {...props({ data: { height: [], weight: [], limited: [] } })}/>);
    expect(screen.getByText('暂无日期、数值和单位完整的身高记录')).toBeTruthy();
    expect(screen.queryByText('72.6')).toBeNull();
});

it('suppresses latest and growth summaries while coverage is incomplete but retains proven measurements', () => {
    const { container } = render(<GrowthCurve {...props({ data: { height: [analysis(height.groups, false)], weight: [], limited: [] } })}/>);
    expect(container.querySelector('.growth-curve__latest')?.textContent).toBe('—cm');
    expect(container.querySelectorAll('[data-measurement]')).toHaveLength(3);
    expect(screen.queryByText('+2.4 cm')).toBeNull();
    expect(screen.getByText(/部分记录尚未完整核验/)).toBeTruthy();
});

it('discards supplied measurements for a non-person ownership scope', () => {
    const { container } = render(<GrowthCurve {...props({ ownership: 'unassigned' })}/>);
    expect(container.querySelectorAll('[data-measurement]')).toHaveLength(0);
    expect(screen.getByText('所选归属不纳入确定统计')).toBeTruthy();
});

it('supports remote entry, metric focus and scrolling from the controls', () => {
    const onUp = vi.fn(), onDown = vi.fn(), onLeft = vi.fn(), onRead = vi.fn();
    render(<GrowthCurve {...props({ onUp, onDown, onLeft, onRead })}/>);
    const height = screen.getByRole('button', { name: '身高' }), weight = screen.getByRole('button', { name: '体重' });
    fireEvent.keyDown(height, { key: 'ArrowUp' }); expect(onUp).toHaveBeenCalledOnce();
    fireEvent.keyDown(height, { key: 'ArrowLeft' }); expect(onLeft).toHaveBeenCalledOnce();
    fireEvent.keyDown(height, { key: 'ArrowRight' }); expect(document.activeElement).toBe(weight);
    fireEvent.keyDown(weight, { key: 'Enter' }); expect(weight.getAttribute('aria-pressed')).toBe('true');
    fireEvent.keyDown(weight, { key: 'ArrowDown' }); expect(onRead).toHaveBeenCalledOnce();
    fireEvent.keyDown(weight, { key: 'ArrowRight' }); expect(onRead).toHaveBeenCalledTimes(2);
    expect(onDown).not.toHaveBeenCalled();
});

it('uses a verified synthetic profile for WHO reference lines and weight comparisons', () => {
    const { container } = render(<GrowthCurve {...props({ profile: { entity_id: 'synthetic-person', birth_date: '2026-01-01', sex: 'female' }, data: { height: [], weight: [analysis([{ metric: 'weight', unit: 'kg', value: '7.6', date: '2026-07-01' }, { metric: 'weight', unit: 'kg', value: '8.2', date: '2026-10-01' }])], limited: [] } })}/>);
    fireEvent.click(screen.getByRole('button', { name: '体重' }));
    expect(screen.getByText('WHO 标准中位数')).toBeTruthy();
    expect(screen.getByText('P3–P97 参考区间')).toBeTruthy();
    expect(container.querySelector('[data-who-band]')).toBeTruthy();
    expect(container.querySelector('[data-who-median]')).toBeTruthy();
    expect(container.querySelector('[data-axis="x"]')?.textContent).toContain('月龄');
    expect(screen.getByText('与标准中位数')).toBeTruthy();
    expect(screen.getByText('同龄百分位')).toBeTruthy();
    expect(screen.getByText('标准同期增长')).toBeTruthy();
    expect(screen.queryByText(/缺少出生日期与性别/)).toBeNull();
});

it('requires a confirmed height posture before showing WHO height comparisons', () => {
    const { container } = render(<GrowthCurve {...props({ profile: { entity_id: 'synthetic-person', birth_date: '2026-01-01', sex: 'female' } })}/>);
    expect(container.querySelector('[data-who-median]')).toBeTruthy();
    expect(screen.getByText(/测量姿势待确认/)).toBeTruthy();
    expect(screen.queryByText(/P3–P97 参考区间内/)).toBeNull();
    const percentile = [...container.querySelectorAll('.growth-curve__stats > div')].find(row => row.textContent?.includes('同龄百分位'));
    expect(percentile?.querySelector('dd')?.textContent).toBe('—');
});

it('shows a verified WHO reference while the first actual measurement is still missing', () => {
    vi.useFakeTimers({ toFake: ['Date'] });
    vi.setSystemTime(new Date('2026-10-21T12:00:00Z'));
    try {
        const { container } = render(<GrowthCurve {...props({ profile: { entity_id: 'synthetic-person', birth_date: '2026-01-01', sex: 'female', height_measurement: 'length' }, data: { height: [], weight: [], limited: [] } })}/>);
        expect(container.querySelector('[data-who-band]')).toBeTruthy();
        expect(container.querySelector('[data-who-median]')).toBeTruthy();
        expect(container.querySelectorAll('[data-measurement]')).toHaveLength(0);
        expect(container.querySelectorAll('[data-curve]')).toHaveLength(0);
        expect(container.querySelector('.growth-curve__latest')?.textContent).toBe('—cm');
        expect(screen.getByText(/当前约.*月龄 · 暂无实测记录/)).toBeTruthy();
        expect(screen.queryByText(/P3–P97 参考区间内/)).toBeNull();
    } finally { vi.useRealTimers(); }
});
