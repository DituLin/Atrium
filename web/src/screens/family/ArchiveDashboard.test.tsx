import { fireEvent, render, screen, within } from '@testing-library/react';
import { expect, it, vi } from 'vitest';
import { ArchiveDashboard, type ArchiveDashboardProps } from './ArchiveDashboard';
import type { ArchiveAnalysis, ArchiveOverview } from '../../types/archives';

const overview: ArchiveOverview = {
    generation: '1:2', total: 128, confirmed: 124, pending: 4,
    domains: [{ domain: 'health', count: 56 }, { domain: 'finance', count: 34 }, { domain: 'education', count: 22 }, { domain: 'property', count: 16 }],
    entities: [], ownership: { household: 19, unassigned: 5, waiting: 3 }, coverage: { complete: true },
};
const props = (more: Partial<ArchiveDashboardProps> = {}): ArchiveDashboardProps => ({ overview, milk: null, milkPeriod: 'day', growth: null, finance: null, personName: null, busy: false, timezone: 'Asia/Singapore', onMilkPeriod: () => {}, onLeft: () => {}, onUp: () => {}, onDown: () => {}, onRead: () => {}, ...more });

it('preserves all original dashboard card positions while unavailable data stays empty', () => {
    const { container } = render(<ArchiveDashboard {...props()}/>);
    expect(container.querySelectorAll('.archive-dashboard__milk')).toHaveLength(1);
    expect(container.querySelectorAll('.archive-dashboard__mix')).toHaveLength(1);
    expect(container.querySelectorAll('.archive-dashboard__mini')).toHaveLength(3);
    expect(screen.getByRole('heading', { name: '身高' })).toBeTruthy();
    expect(screen.getByRole('heading', { name: '体重' })).toBeTruthy();
    expect(screen.getByRole('heading', { name: '财务金额' })).toBeTruthy();
    expect(screen.queryByText('830')).toBeNull();
    expect(container.querySelectorAll('[data-curve]')).toHaveLength(0);
});

it('shows household overview totals independently from domain emphasis', () => {
    const { container } = render(<ArchiveDashboard {...props({ domain: 'health' })}/>);
    const mix = screen.getByRole('button', { name: /档案构成/ });
    expect(within(mix).getByText('128')).toBeTruthy();
    expect(mix.textContent).toContain('124 份已确认 · 4 份待确认');
    expect(mix.textContent).toContain('44%');
    const rows = [...container.querySelectorAll('.archive-dashboard__domain')];
    expect(rows.find(row => row.getAttribute('data-domain') === 'health')?.getAttribute('data-dimmed')).toBe('false');
    expect(rows.filter(row => row.getAttribute('data-dimmed') === 'true')).toHaveLength(3);
    expect(screen.getByText(/不随当前分页变化/)).toBeTruthy();
});

it('switches milk periods and moves the remote focus spatially through the grid', () => {
    const onMilkPeriod = vi.fn(), onLeft = vi.fn(), onDown = vi.fn();
    render(<ArchiveDashboard {...props({ onMilkPeriod, onLeft, onDown })}/>);
    const day = screen.getByRole('button', { name: '日' }), month = screen.getByRole('button', { name: '月' });
    fireEvent.click(screen.getByRole('button', { name: '周' }));
    expect(onMilkPeriod).toHaveBeenCalledWith('week');
    fireEvent.keyDown(day, { key: 'ArrowLeft' });
    expect(onLeft).toHaveBeenCalledOnce();
    fireEvent.keyDown(month, { key: 'ArrowRight' });
    expect(document.activeElement?.textContent).toContain('档案构成');
    fireEvent.keyDown(document.activeElement!, { key: 'ArrowDown' });
    expect(document.activeElement?.textContent).toBe('身高');
    fireEvent.keyDown(screen.getByRole('button',{name:'财务金额'}), { key: 'ArrowDown' });
    expect(onDown).toHaveBeenCalledOnce();
});

it('marks overview coverage limits and never presents an unverified zero as the total', () => {
    const { rerender } = render(<ArchiveDashboard {...props({ overview: null })}/>);
    expect(screen.getByRole('button', { name: /档案构成/ }).textContent).toContain('—');
    rerender(<ArchiveDashboard {...props({ overview: { ...overview, coverage: { complete: false, reason: 'index_pending' } } })}/>);
    expect(screen.getByText('档案统计尚未完整核验')).toBeTruthy();
});

it('renders the proven daily total, real measurement change and separated finance currencies', () => {
    const analysis = (groups: ArchiveAnalysis['groups']): ArchiveAnalysis => ({ snapshot_id: 'test', data_epoch: 1, generation: 2, projection: 'public', algorithm_version: 'test', groups, coverage: { complete: true, included: groups.length, excluded: 0, reasons: [] } });
    const { container } = render(<ArchiveDashboard {...props({
        personName: '合成宝宝',
        milk: { dates: ['2026-10-21'], generation: '1:2', complete: true, totals: [{ metric: 'volume', unit: 'ml', value: '260' }], groups: [{ metric: 'volume', unit: 'ml', value: '120', date: '2026-10-21T08:00:00+08:00' }, { metric: 'volume', unit: 'ml', value: '140', date: '2026-10-21T18:30:00+08:00' }] },
        growth: { height: [analysis([{ metric: 'height', unit: 'cm', value: '71.9', date: '2026-09-21' }, { metric: 'height', unit: 'cm', value: '72.6', date: '2026-10-13' }])], weight: [], limited: [] },
        finance: { months: ['2026-09', '2026-10'], generation: '1:2', complete: true, groups: [{ metric: 'amount', currency: 'CNY', value: '11780', date: '2026-09-01' }, { metric: 'amount', currency: 'CNY', value: '12480', date: '2026-10-01' }, { metric: 'amount', currency: 'USD', value: '30', date: '2026-10-01' }] },
    })}/>);
    const milk = container.querySelector('.archive-dashboard__milk')!;
    expect(milk.textContent).toContain('260');
    expect(milk.textContent).toContain('2 次');
    expect(milk.textContent).toContain('最新 18:30 · 140 ml');
    const height = screen.getByRole('button', { name: '身高 · 合成宝宝' });
    expect(height.textContent).toContain('+0.7 cm');
    const finance = screen.getByRole('button', { name: '财务金额' });
    expect(finance.textContent).toContain('12,480');
    expect(finance.textContent).toContain('+5.94%');
    expect(finance.textContent).toContain('CNY');
    expect(finance.textContent).toContain('USD');
    expect(finance.querySelectorAll('.archive-dashboard__series')).toHaveLength(2);
});

it('keeps unknown domains as a gray other category against the full total', () => {
    const { container } = render(<ArchiveDashboard {...props({ overview: { ...overview, total: 10, confirmed: 8, pending: 2, domains: [{ domain: 'health', count: 7 }, { domain: 'unknown', count: 2 }, { domain: 'other', count: 1 }] } })}/>);
    const other = container.querySelector('.archive-dashboard__domain[data-domain="other"]')!;
    expect(other.textContent).toBe('其他330%');
    const segment = container.querySelector('.archive-dashboard__segment.archive-dashboard__domain--other') as HTMLElement;
    expect(segment.style.flexGrow).toBe('3');
    const health = container.querySelector('.archive-dashboard__domain[data-domain="health"]')!;
    expect(health.textContent).toBe('健康770%');
});

it('preserves an uncovered portion of the composition instead of inflating known domains', () => {
    const { container } = render(<ArchiveDashboard {...props({ overview: { ...overview, total: 10, confirmed: 8, pending: 2, domains: [{ domain: 'health', count: 7 }], coverage: { complete: false } } })}/>);
    const uncounted = container.querySelector('.archive-dashboard__segment--uncovered') as HTMLElement;
    expect(uncounted.style.flexGrow).toBe('3');
    expect(container.querySelector('.archive-dashboard__domain[data-domain="other"]')).toBeNull();
    expect(screen.getByText('另有 3 份尚未计入领域构成')).toBeTruthy();
    expect(screen.getByText('档案统计尚未完整核验')).toBeTruthy();
});

it('labels property-scoped milk records with their filter instead of the whole family', () => {
    render(<ArchiveDashboard {...props({ scopeName: '测试屋', personName: null, milk: { dates: ['2026-10-21'], generation: '1:2', complete: true, totals: [{ metric: 'volume', unit: 'ml', value: '120' }], groups: [{ metric: 'volume', unit: 'ml', value: '120', date: '2026-10-21T08:00:00+08:00' }] } })}/>);
    expect(screen.getByRole('heading', { name: '奶量 · 测试屋' })).toBeTruthy();
    expect(screen.getByRole('img', { name: /奶量 · 测试屋/ })).toBeTruthy();
    expect(screen.getAllByText('选择一位家人查看成长记录')).toHaveLength(2);
});

it('inserts a full-row growth curve immediately below milk and composition, ahead of the small cards',()=>{
 const {container}=render(<ArchiveDashboard {...props()}/>);
 const cards=[...container.querySelector('.archive-dashboard__grid')!.children];
 expect(cards).toHaveLength(6);expect(cards[2]!.classList.contains('growth-curve')).toBe(true);expect(cards[3]!.classList.contains('archive-dashboard__mini--height')).toBe(true);
 expect(screen.getByRole('heading',{name:'生长曲线'})).toBeTruthy();
 const day=screen.getByRole('button',{name:'日'});day.focus();fireEvent.keyDown(day,{key:'ArrowDown'});const curve=within(screen.getByRole('article',{name:'生长曲线'}));expect(document.activeElement).toBe(curve.getByRole('button',{name:'身高'}));
 fireEvent.keyDown(curve.getByRole('button',{name:'体重'}),{key:'ArrowUp'});expect(document.activeElement).toBe(day);
});
