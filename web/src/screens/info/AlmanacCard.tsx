import { forwardRef, useMemo } from 'react';
import type { ComponentPropsWithoutRef } from 'react';
import { RemoteButton } from '../../ui/RemoteButton';
import type { FamilyDay } from './familyDate';
import { presentAlmanac } from './almanacPresentation';

type Props = Omit<ComponentPropsWithoutRef<typeof RemoteButton>, 'children'> & { day: FamilyDay | null };

export const AlmanacCard = forwardRef<HTMLButtonElement, Props>(function AlmanacCard({ day, ...props }, ref) {
  const year = day?.year, month = day?.month, date = day?.day;
  // Minute/second ticks never rebuild calendar cells or solar-term arithmetic.
  const model = useMemo(() => year !== undefined && month !== undefined && date !== undefined
    ? presentAlmanac({ year, month, day: date }) : null, [year, month, date]);
  const info = model?.info;
  return <RemoteButton {...props} className="today-card today-almanac" aria-label="万年历" title="按确定刷新简报" ref={ref}>
    <span className="today-card__heading">
      <span className="today-card__eyebrow">万年历</span>
      {model ? <span className="today-almanac__ordinal">{info ? `${info.year} · ` : ''}第 {model.weekNumber} 周 · 第 {model.dayOfYear} 天</span> : null}
    </span>
    {day && model ? <>
      <span className="today-almanac__body">
        <span className="today-almanac__gregorian">
          <span className="today-almanac__month">{model.month}</span>
          <span className="today-almanac__date" data-testid="almanac-date">{day.day}</span>
          <span className="today-almanac__weekday">{day.weekday}</span>
        </span>
        <span className="today-almanac__divider" aria-hidden="true" />
        {info ? <span className="today-almanac__lunisolar">
          <span className="today-almanac__lunar" aria-label={`农历${info.lunar}`}>
            <span className="today-almanac__seal" aria-hidden="true">农<br />历</span>
            <span className="today-almanac__lunar-date" aria-hidden="true">{info.lunar}</span>
          </span>
          <span className="today-almanac__ganzhi"><span>{info.year}</span> <span>{info.day}</span></span>
        </span> : <span className="today-almanac__range">此日期超出农历推算范围</span>}
        <span className="today-almanac__divider" aria-hidden="true" />
        <span className="today-almanac__details">
          <span className="today-almanac__custom" aria-label="宜"><span className="today-almanac__custom-label">宜</span><span className="today-almanac__unavailable">暂无资料</span></span>
          <span className="today-almanac__custom today-almanac__custom--avoid" aria-label="忌"><span className="today-almanac__custom-label">忌</span><span className="today-almanac__unavailable">暂无资料</span></span>
          {info?.today.length ? <span className="today-almanac__festivals" role="list" aria-label="今日节日与节气">{info.today.map(name => <span role="listitem" key={name}>{name}</span>)}</span> : null}
          {model.term ? <span className="today-almanac__term">
            <span className="today-almanac__term-heading"><span>距{model.term.next.name}还有 {model.term.remaining} 天</span><span>{model.term.elapsed} / {model.term.total}</span></span>
            <span className="today-almanac__progress" role="progressbar" aria-label="节气进度" aria-valuemin={0} aria-valuemax={model.term.total} aria-valuenow={model.term.elapsed}>
              <span style={{ width: `${model.term.percent}%` }} />
            </span>
            <span className="today-almanac__term-dates"><span>{model.term.previous.name} {model.term.previous.month}/{model.term.previous.day}</span><span>{model.term.next.name} {model.term.next.month}/{model.term.next.day}</span></span>
          </span> : null}
        </span>
      </span>
      <span className="today-almanac__week" role="list" aria-label="本周日历">{model.week.map(item => <span className={`today-almanac__week-day${item.weekend ? ' is-weekend' : ''}${item.special ? ' is-special' : ''}`} role="listitem" aria-current={item.today ? 'date' : undefined} data-date={item.key} key={item.key}>
        <span className="today-almanac__week-name">{item.weekday}</span>
        <span className="today-almanac__week-date">{item.day}</span>
        <span className="today-almanac__week-lunar">{item.lunar}</span>
      </span>)}</span>
    </> : <span className="today-almanac__waiting">等待家庭时间</span>}
  </RemoteButton>;
});
