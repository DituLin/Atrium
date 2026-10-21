import { describe, expect, it } from 'vitest';
import type { ArchiveAnalysis } from '../../types/archives';
import type { MilkTrendData } from './feedingTrend';
import type { FinanceTrendData } from './financeTrend';
import type { GrowthData } from './growthTrend';
import { summarizeFinance, summarizeGrowth, summarizeMilk } from './dashboardStats';

const analysis = (groups: ArchiveAnalysis['groups'], complete = true) => ({ data_epoch: 1, generation: 1, groups, coverage: { complete } } as ArchiveAnalysis);
const milk = (groups: ArchiveAnalysis['groups'], totals?: ArchiveAnalysis['groups']): MilkTrendData => ({ dates: ['2026-10-19', '2026-10-20', '2026-10-21'], generation: '1:1', complete: true, groups, totals });
const finance = (groups: ArchiveAnalysis['groups']): FinanceTrendData => ({ months: ['2026-05', '2026-06', '2026-07', '2026-08', '2026-09', '2026-10'], generation: '1:1', complete: true, groups });
const growth = (groups: ArchiveAnalysis['groups']): GrowthData => ({ height: [analysis(groups)], weight: [], limited: [] });

describe('milk dashboard statistics', () => {
    it('uses the exact Core sum for today, preserving decimal spelling and counting feeds', () => {
        const data = milk([{ metric: 'volume', value: '0.1', unit: 'ml', date: '2026-10-21T01:00:00Z' }, { metric: 'volume', value: '0.2', unit: 'ml', date: '2026-10-21T02:00:00Z' }], [{ metric: 'volume', value: '0.3000', unit: 'ml' }]);
        expect(summarizeMilk(data, 'day')[0]).toMatchObject({ headline: '0.3000', count: 2, latestValue: '0.2', recorded: 1, expected: 1, complete: true });
    });
    it('averages recorded daily totals without filling missing days with zero', () => {
        const data = milk([{ metric: 'volume', value: '0.1', unit: 'ml', date: '2026-10-19' }, { metric: 'volume', value: '0.2', unit: 'ml', date: '2026-10-21' }]);
        expect(summarizeMilk(data, 'week')[0]).toMatchObject({ headline: '0.15', recorded: 2, expected: 3, complete: true });
    });
    it('rounds derived daily averages to two decimals with decimal half-up rounding', () => {
        const data = milk([{ metric: 'volume', value: '0.1', unit: 'ml', date: '2026-10-19' }, { metric: 'volume', value: '0.11', unit: 'ml', date: '2026-10-21' }]);
        expect(summarizeMilk(data, 'month')[0]?.headline).toBe('0.11');
    });
    it('retains the exact Core decimal when a period has only one recorded day', () => {
        expect(summarizeMilk(milk([{ metric: 'volume', value: '9007199254740991', unit: 'ml', date: '2026-10-21' }]), 'week')[0]).toMatchObject({ headline: '9007199254740991', complete: true });
    });
    it('separates metric, unit, currency and sample instead of merging incompatible values', () => {
        const data = milk([
            { metric: 'volume', value: '1', unit: 'ml', date: '2026-10-21' }, { metric: 'volume', value: '2', unit: 'l', date: '2026-10-21' },
            { metric: 'formula', value: '3', unit: 'ml', date: '2026-10-21' }, { metric: 'volume', value: '4', unit: 'ml', sample: '合成A', date: '2026-10-21' },
            { metric: 'volume', value: '5', unit: 'ml', currency: 'CNY', date: '2026-10-21' },
        ]);
        expect(summarizeMilk(data, 'week')).toHaveLength(5);
        expect(new Set(summarizeMilk(data, 'week').map(s => s.key)).size).toBe(5);
    });
    it('does not invent a total when no Core day sum is available', () => {
        const data = milk([{ metric: 'volume', value: '120', unit: 'ml', date: '2026-10-21T01:00:00Z' }]);
        expect(summarizeMilk(data, 'day')[0]).toMatchObject({ headline: null, complete: false });
    });
    it('refuses invalid dates, duplicate daily totals and unsafe scaled integers', () => {
        for (const groups of [
            [{ metric: 'volume', value: '1', unit: 'ml', date: '2026-02-30' }],
            [{ metric: 'volume', value: '1', unit: 'ml', date: '2026-10-21T01:00:00' }],
            [{ metric: 'volume', value: '9007199254740992', unit: 'ml', date: '2026-10-21' }],
            [{ metric: 'volume', value: '1', unit: 'ml', date: '2026-10-21' }, { metric: 'volume', value: '2', unit: 'ml', date: '2026-10-21' }],
        ]) expect(summarizeMilk(milk(groups), 'week')[0]).toMatchObject({ headline: null, complete: false });
    });
});
describe('finance dashboard statistics', () => {
    it('shows the current month and exact difference, with percent rounded to two decimals', () => {
        const data = finance([{ metric: 'amount', value: '0.1', currency: 'CNY', date: '2026-09-01' }, { metric: 'amount', value: '0.3', currency: 'CNY', date: '2026-10-01' }]);
        expect(summarizeFinance(data)[0]).toMatchObject({ headline: '0.3', change: '0.2', changePercent: '200', recorded: 2, expected: 6, complete: true });
    });
    it('keeps unknown current month empty and never compares with a nonadjacent month', () => {
        const historical = finance([{ metric: 'amount', value: '30', currency: 'CNY', date: '2026-08-01' }]);
        expect(summarizeFinance(historical)[0]).toMatchObject({ headline: null, change: null, changePercent: null, recorded: 1 });
        historical.groups.push({ metric: 'amount', value: '40', currency: 'CNY', date: '2026-10-01' });
        expect(summarizeFinance(historical)[0]).toMatchObject({ headline: '40', change: null, changePercent: null, recorded: 2 });
    });
    it('separates currencies, samples and units and omits percent for a zero previous month', () => {
        const data = finance([
            { metric: 'amount', value: '0', currency: 'CNY', date: '2026-09-01' }, { metric: 'amount', value: '2', currency: 'CNY', date: '2026-10-01' },
            { metric: 'amount', value: '3', currency: 'USD', date: '2026-10-01' }, { metric: 'amount', value: '4', currency: 'CNY', sample: '合成', date: '2026-10-01' },
            { metric: 'amount', value: '5', currency: 'CNY', unit: '元', date: '2026-10-01' },
        ]);
        expect(summarizeFinance(data)).toHaveLength(4);
        expect(summarizeFinance(data)[0]).toMatchObject({ headline: '2', change: '2', changePercent: null });
    });
    it('marks limited input and unrepresentable derived arithmetic incomplete', () => {
        const data = finance([{ metric: 'amount', value: '9007199254740991', currency: 'CNY', date: '2026-09-01' }, { metric: 'amount', value: '-9007199254740991', currency: 'CNY', date: '2026-10-01' }]);
        expect(summarizeFinance(data)[0]).toMatchObject({ change: null, changePercent: null, complete: false });
        data.complete = false;
        expect(summarizeFinance(data)[0]?.complete).toBe(false);
    });
});
describe('growth dashboard statistics', () => {
    it('uses the latest two chronological measurements and preserves the latest Core decimal', () => {
        const data = growth([{ metric: 'height', value: '72.600', unit: 'cm', date: '2026-10-21' }, { metric: 'height', value: '71.9', unit: 'cm', date: '2026-10-10' }]);
        expect(summarizeGrowth(data, 'height')[0]).toMatchObject({ headline: '72.600', change: '0.7', latestDate: '2026-10-21', count: 2, complete: true });
    });
    it('does not compare measurements from different units or samples', () => {
        const data = growth([{ metric: 'height', value: '72.6', unit: 'cm', date: '2026-10-21' }, { metric: 'height', value: '0.7', unit: 'm', date: '2026-10-10' }, { metric: 'height', value: '71.9', unit: 'cm', sample: 'standing', date: '2026-10-10' }]);
        expect(summarizeGrowth(data, 'height')).toHaveLength(3);
        expect(summarizeGrowth(data, 'height').every(s => s.change === null)).toBe(true);
    });
    it('orders literal dates and instants by the household calendar before selecting the latest value', () => {
        const data = growth([{ metric: 'height', value: '70.00', unit: 'cm', date: '2026-10-13' }, { metric: 'height', value: '69.00', unit: 'cm', date: '2026-10-12T23:30:00-07:00' }]);
        expect(summarizeGrowth(data, 'height', 'America/Los_Angeles')[0]).toMatchObject({ headline: '70.00', change: '1', latestDate: '2026-10-13' });
    });
    it('retains a partial warning and does not pick an arbitrary reading for tied latest dates', () => {
        const data = growth([{ metric: 'height', value: '72.6', unit: 'cm', date: '2026-10-21' }, { metric: 'height', value: '72.7', unit: 'cm', date: '2026-10-21' }]);
        data.limited = ['height'];
        expect(summarizeGrowth(data, 'height')[0]).toMatchObject({ headline: null, change: null, complete: false });
    });
    it('does not interpret an unzoned timestamp using the display device timezone', () => {
        const data = growth([{ metric: 'height', value: '70', unit: 'cm', date: '2026-10-21T01:00:00' }]);
        expect(summarizeGrowth(data, 'height')[0]).toMatchObject({ headline: null, complete: false });
    });
    it('returns empty arrays for unavailable data', () => {
        expect(summarizeMilk(null, 'day')).toEqual([]); expect(summarizeFinance(null)).toEqual([]); expect(summarizeGrowth(null, 'weight')).toEqual([]);
    });
});
