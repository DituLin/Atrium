import { expect, it } from 'vitest';
import { calendarAgeLabel } from './calendarAge';
import type { CalendarDay } from './calendarAge';

const day = (date: string): CalendarDay => {
    const [year = 0, month = 0, dateOfMonth = 0] = date.split('-').map(Number);
    return { year, month, day: dateOfMonth };
};

it.each([
    ['2025-08-15', '2026-10-15', '1岁2个月'],
    ['2025-08-15', '2026-10-14', '1岁1个月'],
    ['2025-08-15', '2026-08-15', '1岁0个月'],
    ['2025-08-15', '2026-08-14', '11个月30天'],
    ['2026-02-10', '2026-10-10', '8个月0天'],
    ['2026-02-10', '2026-10-22', '8个月12天'],
    ['2026-02-10', '2026-02-10', '0个月0天'],
    ['2026-02-10', '2026-02-28', '0个月18天'],
    ['2026-01-31', '2026-02-27', '0个月27天'],
    ['2026-01-31', '2026-02-28', '1个月0天'],
    ['2026-01-31', '2026-03-01', '1个月1天'],
    ['2026-01-31', '2026-03-30', '1个月30天'],
    ['2026-01-31', '2026-03-31', '2个月0天'],
    ['2024-02-29', '2024-03-28', '0个月28天'],
    ['2024-02-29', '2024-03-29', '1个月0天'],
    ['2024-02-29', '2025-02-27', '11个月29天'],
    ['2024-02-29', '2025-02-28', '1岁0个月'],
    ['2024-02-29', '2028-02-28', '3岁11个月'],
    ['2024-02-29', '2028-02-29', '4岁0个月'],
])('counts completed calendar age from %s to %s as %s', (birth, today, label) => {
    expect(calendarAgeLabel(birth, day(today))).toBe(label);
});

it.each(['', '2025-1-01', '2025-01', '2025-02-29', '2025-02-30', '2026-13-01', '2026-00-01', '2026-01-00', '2026-01-32', '0000-01-01', '2025-01-01T00:00:00Z'])('rejects incomplete or invalid birth date %s', birth => {
    expect(calendarAgeLabel(birth, day('2026-10-15'))).toBeNull();
});

it('does not assign age before birth or without a valid current family date', () => {
    expect(calendarAgeLabel('2026-10-16', day('2026-10-15'))).toBeNull();
    for (const today of [null, undefined, day('2026-02-30'), { year: Number.NaN, month: 1, day: 1 }, { year: 2026, month: 1.5, day: 1 }]) {
        expect(calendarAgeLabel('2025-01-01', today)).toBeNull();
    }
});
