/**
 * 万年历 card for 今日: the family's Gregorian day with its lunar date, 干支,
 * festivals / solar term of the day and the next solar term. Computed locally
 * from the family date, so it needs no source and never goes stale.
 */

import type { KeyboardEvent } from 'react';
import { forwardRef, useMemo } from 'react';

import { almanac } from '../../core/lunar';
import type { FamilyDay } from './familyDate';

export const AlmanacCard = forwardRef<HTMLElement, {
  day: FamilyDay | null;
  onKeyDown?: (event: KeyboardEvent<HTMLElement>) => void;
}>(function AlmanacCard(props, ref) {
  const { day } = props;
  const year = day?.year, month = day?.month, date = day?.day;
  // Recomputed only when the family date rolls over, not on every clock tick.
  const info = useMemo(() => (year && month && date ? almanac(year, month, date) : null), [year, month, date]);
  return (
    <section className="card info-almanac" aria-label="万年历" tabIndex={0} ref={ref} onKeyDown={props.onKeyDown}>
      <div className="eyebrow">万年历</div>
      {day ? (
        <>
          <div className="info-almanac__solar">
            <span className="info-almanac__day serif">{day.day}</span>
            <span className="info-almanac__meta">
              <span>{day.year}年{day.month}月</span>
              <span>{day.weekday}</span>
            </span>
          </div>
          {info ? (
            <>
              <div className="info-almanac__lunar serif">农历{info.lunar}</div>
              <div className="info-almanac__ganzhi muted">{info.year} · {info.day}</div>
              {info.today.length ? (
                <ul className="info-almanac__chips" aria-label="今日节日与节气">
                  {info.today.map(name => <li className="info-almanac__chip" key={name}>{name}</li>)}
                </ul>
              ) : null}
              {info.nextTerm ? <div className="info-almanac__next muted">距{info.nextTerm.name}还有 {info.nextTerm.inDays} 天</div> : null}
            </>
          ) : <div className="info-almanac__ganzhi muted">此日期超出农历推算范围</div>}
        </>
      ) : <div className="info-almanac__ganzhi muted">等待家庭时间</div>}
    </section>
  );
});
