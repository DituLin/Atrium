import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import { flushSync } from 'react-dom';
import { useApp } from '../app/context';
import { useOverviewDeadline } from '../app/useOverviewDeadline';
import { serverNow } from '../core/clock';
import type { WeatherWidgetPayload } from '../types/api';
import { TopNav, focusTopNav } from '../ui/TopNav';
import { AlmanacCard } from './info/AlmanacCard';
import { WeatherCard } from './info/WeatherCard';
import { familyClockOptions, familyClockTime, familyDay } from './info/familyDate';
import { restoreReturnFocus } from './info/readingRegion';

export function BriefingScreen() {
  const { state, overview, refreshBriefing, dispatch } = useApp();
  // A sleeping TV may return before its first timer or network response. Sample
  // entry and foreground synchronously so even that first calendar is current.
  const [entryNow, setEntryNow] = useState(Date.now);
  useLayoutEffect(() => {
    const foreground = () => {
      if (document.visibilityState !== 'hidden') flushSync(() => setEntryNow(Date.now()));
    };
    document.addEventListener('visibilitychange', foreground);
    return () => document.removeEventListener('visibilitychange', foreground);
  }, []);
  const calendar = useRef<HTMLButtonElement>(null);
  const weatherCard = useRef<HTMLButtonElement>(null);
  const lastCard = useRef<'calendar' | 'weather'>('calendar');
  useEffect(() => { restoreReturnFocus(state.router.restoreFocus, calendar.current); }, [state.router.restoreFocus]);
  const request = useRef(0), mounted = useRef(true), pending = useRef(false);
  const [manual, setManual] = useState<'idle' | 'loading' | 'failed'>('idle');
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; request.current += 1; }; }, []);
  const refresh = () => {
    if (pending.current) return;
    pending.current = true; setManual('loading');
    const generation = ++request.current;
    void (refreshBriefing ? refreshBriefing() : overview.load()).then(() => {
      if (mounted.current && request.current === generation) { pending.current = false; setManual('idle'); }
    }).catch(() => {
      if (mounted.current && request.current === generation) { pending.current = false; setManual('failed'); }
    });
  };
  const { snapshot, status, refreshing } = state.overview;
  const now = serverNow(state.clock, Math.max(state.nowMs, entryNow));
  useOverviewDeadline(snapshot, now, state.clock.offsetMs, dispatch);
  const options = familyClockOptions(state.home, snapshot?.home.timezone);
  const day = familyDay(now, options);
  const timezone = state.home?.home.timezone ?? snapshot?.home.timezone ?? 'UTC';
  const generated = snapshot ? familyClockTime(snapshot.generated_at, timezone) : null;
  const busy = manual === 'loading' || refreshing;
  const failed = status === 'failed' || status === 'stale' || manual === 'failed';
  const feedback = busy ? '正在刷新…' : status === 'stale' || manual === 'failed' ? '暂时无法更新简报'
    : status === 'failed' ? '暂时无法读取简报' : snapshot ? '已更新简报' : '正在读取简报…';
  let weather: WeatherWidgetPayload | null = null;
  for (const widget of state.home?.widgets ?? []) {
    if (widget.type === 'weather' && widget.payload && typeof widget.payload === 'object') {
      weather = widget.payload as WeatherWidgetPayload; break;
    }
  }
  return <div className="page info-today today-page">
    <TopNav onDown={() => (lastCard.current === 'weather' ? weatherCard : calendar).current?.focus()} />
    <div className="page__heading today-page__heading">
      <h1 className="page__title">今日</h1>
      <span className="page__subtitle">万年历 · 天气</span>
      <span className="page__aside today-page__status" role="status"><span className={`dot dot--${failed ? 'warn' : snapshot ? 'ok' : 'none'}`} aria-hidden="true" /><span><span>{feedback}</span>{generated ? <span> · 汇总于 {generated}</span> : null}</span></span>
    </div>
    <main className="page__body today-page__cards">
      <AlmanacCard day={day} ref={calendar} data-return-focus="briefing-almanac" aria-busy={busy} onClick={refresh} onFocus={() => { lastCard.current = 'calendar'; }} onDirection={key => {
        if (key === 'up') focusTopNav();
        if (key === 'right') weatherCard.current?.focus();
      }} />
      <WeatherCard weather={weather} options={options} now={now} ref={weatherCard} data-return-focus="briefing-weather" aria-busy={busy} onClick={refresh} onFocus={() => { lastCard.current = 'weather'; }} onDirection={key => {
        if (key === 'up') focusTopNav();
        if (key === 'left') calendar.current?.focus();
      }} />
    </main>
  </div>;
}
