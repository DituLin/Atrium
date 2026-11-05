export interface CalendarDay { year: number; month: number; day: number }

function dayNumber(day: CalendarDay): number | null {
    if (!Number.isInteger(day.year) || day.year < 1 || day.year > 9999 || !Number.isInteger(day.month)
        || day.month < 1 || day.month > 12 || !Number.isInteger(day.day) || day.day < 1 || day.day > 31) return null;
    const date = new Date(0);
    // setUTCFullYear also handles years 1…99 without Date.UTC's 1900 offset.
    date.setUTCFullYear(day.year, day.month - 1, day.day);
    date.setUTCHours(0, 0, 0, 0);
    return date.getUTCFullYear() === day.year && date.getUTCMonth() + 1 === day.month && date.getUTCDate() === day.day ? date.getTime() : null;
}

/**
 * Completed civil years/months, independent of WHO's average month lengths.
 * Each month anniversary uses the original birth day, clamped to that month's
 * end (Jan 31 → Feb 28/29; Feb 29 → Feb 28 in a non-leap year).
 * Under one year the residual civil days start at the most recent clamped
 * month anniversary; from one year onward only completed years/months show.
 */
export function calendarAgeLabel(birthDate: string, today: CalendarDay | null | undefined): string | null {
    if (!today || !/^\d{4}-\d{2}-\d{2}$/.test(birthDate)) return null;
    const birth = { year: Number(birthDate.slice(0, 4)), month: Number(birthDate.slice(5, 7)), day: Number(birthDate.slice(8, 10)) };
    const born = dayNumber(birth), current = dayNumber(today);
    if (born === null || current === null || current < born) return null;
    const monthEnd = new Date(0);
    monthEnd.setUTCFullYear(today.year, today.month, 0);
    const anniversaryDay = Math.min(birth.day, monthEnd.getUTCDate());
    const completedMonths = (today.year - birth.year) * 12 + today.month - birth.month - (today.day < anniversaryDay ? 1 : 0);
    const years = Math.floor(completedMonths / 12), months = completedMonths % 12;
    if (years) return `${years}岁${months}个月`;
    const anniversary = new Date(0);
    anniversary.setUTCFullYear(birth.year, birth.month + completedMonths, 0);
    anniversary.setUTCDate(Math.min(birth.day, anniversary.getUTCDate()));
    const days = (current - anniversary.getTime()) / 86400000;
    return `${months}个月${days}天`;
}
