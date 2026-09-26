import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import { flushSync } from 'react-dom';
import { useApp } from '../app/context';
import { useOverviewDeadline } from '../app/useOverviewDeadline';
import { projectOverview } from '../app/overview';
import { serverNow } from '../core/clock';
import { RemoteButton } from '../ui/RemoteButton';
import { TopNav, focusTopNav } from '../ui/TopNav';
import { AlmanacCard } from './info/AlmanacCard';
import { StatusRow } from './info/StatusRow';
import { familyClockOptions, familyClockTime, familyDay, familyTime } from './info/familyDate';
import { homeRows } from './info/homeRows';
import { onReadingKey, restoreReturnFocus } from './info/readingRegion';
import { mapRemoteKey } from '../ui/keys';

const NOTICE_EMPTY = { available: '到期的提示会自动隐藏。', stale: '暂时无法更新提示。', loading: '家庭提示尚未完成首次读取。', failed: '暂时无法读取家庭提示。', not_connected: '家庭提示尚未接入。' } as const;

type Projected = ReturnType<typeof projectOverview>;
type NoticeRow = { entry: Projected['entries'][number]; item: Projected['sources']['notice']['items'][number] };

export function BriefingScreen() {
  const { state, overview, dispatch } = useApp();
  // A retained snapshot may outlive the last global tick while another route
  // is open or the TV is suspended. Sample entry before the first DOM commit.
  const [entryNow, setEntryNow] = useState(Date.now);
  useLayoutEffect(() => {
    const foreground = () => {
      if (document.visibilityState !== 'hidden') {
        // Withdraw expired content before the foreground event returns, even
        // when background timers and the pending refresh have not completed.
        flushSync(() => setEntryNow(Date.now()));
      }
    };
    document.addEventListener('visibilitychange', foreground);
    return () => document.removeEventListener('visibilitychange', foreground);
  }, []);
  const almanacCard = useRef<HTMLElement>(null);
  const reading = useRef<HTMLDivElement>(null);
  const houseButton = useRef<HTMLButtonElement>(null);
  const refresh = useRef<HTMLButtonElement>(null);
  useEffect(() => { restoreReturnFocus(state.router.restoreFocus, reading.current); }, [state.router.restoreFocus]);
  const { snapshot, status, refreshing } = state.overview;
  const now = serverNow(state.clock, Math.max(state.nowMs, entryNow));
  useOverviewDeadline(snapshot, now, state.clock.offsetMs, dispatch);
  const transportFailed = status === 'stale';
  const projected = snapshot ? projectOverview(snapshot.sources, now, transportFailed) : null;
  const timezone = snapshot?.home.timezone ?? 'UTC';
  const day = familyDay(now, familyClockOptions(state.home, snapshot?.home.timezone));
  const generated = snapshot ? familyClockTime(snapshot.generated_at, timezone) : null;
  const feedback = refreshing ? '正在刷新…' : status === 'stale' ? '暂时无法更新 · 显示仍有效的旧数据' : status === 'failed' ? '暂时无法读取简报' : snapshot ? '已更新简报' : '正在读取简报…';
  // A plain loop: Array.prototype.flatMap is missing from the TCL TV's Chrome 66.
  const notices: NoticeRow[] = [];
  for (const entry of projected ? projected.entries : []) {
    if (entry.kind !== 'notice' || !projected) continue;
    const item = projected.sources.notice.items.find(candidate => candidate.id === entry.item_id);
    if (item) notices.push({ entry, item });
  }
  const noticeSource = projected?.sources.notice;
  return <div className="page info-today">
    <TopNav onDown={() => reading.current?.focus()} />
    <div className="page__heading">
      <h1 className="page__title">今日</h1>
      {day ? <span className="page__subtitle">{day.label}</span> : null}
      {generated ? <span className="page__aside">汇总于 {generated}</span> : null}
    </div>
    <main className="page__body info-today__grid">
      <AlmanacCard day={day} ref={almanacCard} onKeyDown={event => {
        const key = mapRemoteKey(event);
        if (!key || key === 'back' || key === 'enter') return;
        event.preventDefault(); event.stopPropagation();
        if (key === 'up') focusTopNav();
        if (key === 'right') reading.current?.focus();
        if (key === 'down') refresh.current?.focus();
      }} />
      <section className="info-today__notices">
        <h2 className="page__section-title">家庭提示</h2>
        <div className="info-today__reading" role="region" aria-label="今日事项" tabIndex={0} ref={reading} onKeyDown={event => onReadingKey(event, {
          left: () => almanacCard.current?.focus(), right: () => houseButton.current?.focus(),
          top: () => focusTopNav(), bottom: () => houseButton.current?.focus(),
        })}>
          {!projected ? <p className="info-empty">{status === 'failed' ? '暂时无法读取简报' : '正在读取今日事项…'}</p> : notices.length === 0
            ? <section className="info-empty"><h3 className="info-empty__title serif">当前没有可展示的提示</h3><p className="muted">{NOTICE_EMPTY[noticeSource!.availability]}</p></section>
            : notices.map(({ entry, item }) => <article className="info-notice" key={entry.id} data-entry={entry.id}>
              <p className="info-notice__text">{item.text}</p>
              <p className="info-notice__meta">
                {noticeSource!.availability === 'stale' ? <span>旧数据</span> : null}
                {item.valid_from ? <span>开始 {familyTime(item.valid_from, timezone)}</span> : null}
                {item.valid_until ? <span>有效至 {familyTime(item.valid_until, timezone)}</span> : null}
                {item.updated_at ? <span>更新于 {familyTime(item.updated_at, timezone)}</span> : null}
                <span>{noticeSource!.source_label}</span>
              </p>
            </article>)}
        </div>
      </section>
      <aside className="info-today__home" aria-label="简报来源">
        <h2 className="page__section-title">家里</h2>
        <div className="row-list">
          {homeRows(projected?.sources ?? null, now, transportFailed).map(row => <StatusRow key={row.key} tone={row.tone} label={row.label} value={row.value} />)}
        </div>
        <div className="info-actions">
          <RemoteButton className="btn" ref={houseButton} data-return-focus="briefing-house" onClick={() => dispatch({ type: 'router.navigate', route: { name: 'house' }, sourceFocus: 'briefing-house' })} onDirection={key => {
            if (key === 'left') reading.current?.focus();
            if (key === 'up') focusTopNav();
            if (key === 'down' || key === 'right') refresh.current?.focus();
          }}>查看房屋</RemoteButton>
          <RemoteButton className="btn" ref={refresh} onClick={() => { void overview.load().catch(() => {}); }} onDirection={key => {
            if (key === 'left') houseButton.current?.focus();
            if (key === 'up') focusTopNav();
          }}>刷新简报</RemoteButton>
        </div>
      </aside>
    </main>
    <footer className="hints">
      <span>方向键 移动</span><span>↑↓ 滚动提示</span><span>返回 回到首页</span>
      <span className="hints__end" role="status">{feedback}</span>
    </footer>
  </div>;
}
