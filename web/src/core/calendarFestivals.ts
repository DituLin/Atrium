/** Shared civil/lunar festival labels, independent of the calendar engine. */
const LUNAR_FESTIVALS: Record<string, string> = {
  '1-1': '春节', '1-15': '元宵节', '2-2': '龙抬头', '5-5': '端午节', '7-7': '七夕',
  '7-15': '中元节', '8-15': '中秋节', '9-9': '重阳节', '12-8': '腊八节', '12-23': '小年',
};
const SOLAR_FESTIVALS: Record<string, string> = {
  '1-1': '元旦', '5-1': '劳动节', '10-1': '国庆节',
};

export function calendarFestivals(month: number, day: number, lunar: { month: number; day: number; leap: boolean; monthLength: number }): string[] {
  const labels: string[] = [];
  if (!lunar.leap) {
    const festival = LUNAR_FESTIVALS[`${lunar.month}-${lunar.day}`];
    if (festival) labels.push(festival);
    if (lunar.month === 12 && lunar.day === lunar.monthLength) labels.push('除夕');
  }
  const solar = SOLAR_FESTIVALS[`${month}-${day}`];
  if (solar) labels.push(solar);
  return labels;
}
