import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import { flushSync } from 'react-dom';
import { useApp } from '../app/context';
import { useOverviewDeadline } from '../app/useOverviewDeadline';
import { projectOverview } from '../app/overview';
import { serverNow } from '../core/clock';
import type { FamilyAvailability, FamilySourceSnapshot } from '../types/api';
import { Masthead, PrimaryNav, focusPrimaryNav } from '../ui/PrimaryNav';
import { RemoteButton } from '../ui/RemoteButton';
import { mapRemoteKey } from '../ui/keys';

const HEALTH = { online: '在线', offline: '离线', degraded: '异常', unknown: '尚未确认' };
const AVAILABILITY: Record<FamilyAvailability, string> = { available: '已接入', stale: '状态待更新', loading: '尚未完成首次读取', failed: '暂时无法读取', not_connected: '未接入' };
function time(value: string | null, timezone: string): string {
  if (!value) return '尚无记录';
  try { return new Intl.DateTimeFormat('zh-CN', { timeZone: timezone, month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false }).format(new Date(value)); }
  catch { return '时间待确认'; }
}
function summary<T>(source: FamilySourceSnapshot<T>): string {
  return source.availability === 'available' ? `${source.items.length} 条有效内容` : AVAILABILITY[source.availability];
}
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
  const reading = useRef<HTMLDivElement>(null);
  const houseButton = useRef<HTMLButtonElement>(null);
  const refresh = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    const source = state.router.restoreFocus
      ? Array.from(document.querySelectorAll<HTMLElement>('[data-return-focus]')).find(element => element.dataset.returnFocus === state.router.restoreFocus) : null;
    (source ?? reading.current)?.focus();
  }, [state.router.restoreFocus]);
  const { snapshot, status, refreshing } = state.overview;
  const now = serverNow(state.clock, Math.max(state.nowMs, entryNow));
  useOverviewDeadline(snapshot, now, state.clock.offsetMs, dispatch);
  const projected = snapshot ? projectOverview(snapshot.sources, now, status === 'stale') : null;
  const timezone = snapshot?.home.timezone ?? 'UTC';
  let date = '';
  if (snapshot) { try { date = new Intl.DateTimeFormat('zh-CN', { timeZone: timezone, month: 'long', day: 'numeric', weekday: 'long' }).format(new Date(now)); } catch { /* Invalid time zones never invent a family date. */ } }
  const feedback = refreshing ? '正在刷新…' : status === 'stale' ? '暂时无法更新 · 显示仍有效的旧数据' : status === 'failed' ? '暂时无法读取简报' : snapshot ? '已更新简报' : '正在读取简报…';
  const counts = new Map<string, number>();
  for (const source of projected?.sources.nas ?? []) {
    const label = source.availability === 'available' ? HEALTH[source.items[0]?.health ?? 'unknown'] : AVAILABILITY[source.availability];
    counts.set(label, (counts.get(label) ?? 0) + 1);
  }
  const nasSummary = projected ? projected.sources.nas.length ? `${projected.sources.nas.length} 个来源 · ${Array.from(counts, ([label, count]) => `${label} ${count}`).join(' · ')}` : '没有可展示的来源' : '等待读取';
  return <div className="screen screen--briefing">
    <Masthead /><header className="briefing__heading"><h1 className="photos__title">今日</h1><p>{date}</p></header>
    <main className="briefing__main">
      <div className="briefing__columns">
        <div className="briefing__reading" role="region" aria-label="今日事项" tabIndex={0} ref={reading} onKeyDown={event => {
          const key = mapRemoteKey(event);
          if (!key || key === 'back' || key === 'enter') return;
          event.preventDefault(); event.stopPropagation();
          const element = event.currentTarget;
          if (key === 'left') focusPrimaryNav('briefing');
          if (key === 'right') houseButton.current?.focus();
          if (key === 'up') element.scrollTop = Math.max(0, element.scrollTop - element.clientHeight * .7);
          if (key === 'down') {
            if (element.scrollTop + element.clientHeight >= element.scrollHeight - 1) refresh.current?.focus();
            else element.scrollTop += element.clientHeight * .7;
          }
        }}>
          {!projected ? <p>{status === 'failed' ? '暂时无法读取简报' : '正在读取今日事项…'}</p> : projected.entries.length === 0
            ? <section className="briefing__empty"><h2>当前没有可展示的提示</h2><p>可在右侧查看各类来源的接入情况。</p></section>
            : projected.entries.map(entry => {
              if (entry.kind === 'notice') {
                const source = projected.sources.notice;
                const item = source.items.find(item => item.id === entry.item_id);
                if (!item) return null;
                return <section className="briefing__item" key={entry.id} data-entry={entry.id}>
                  <p className="briefing__eyebrow">家庭提示{source.availability === 'stale' ? ' · 旧数据' : ' · 当前有效'}</p>
                  <h2>{source.source_label}</h2><p className="briefing__text">{item.text}</p>
                  <dl className="briefing__times"><div><dt>内容更新</dt><dd>{time(item.updated_at, timezone)}</dd></div>
                    {item.valid_from ? <div><dt>开始</dt><dd>{time(item.valid_from, timezone)}</dd></div> : null}
                    {item.valid_until ? <div><dt>有效至</dt><dd>{time(item.valid_until, timezone)}</dd></div> : null}
                  </dl>
                </section>;
              }
              const source = projected.sources.nas.find(source => source.source_id === entry.source_id);
              if (!source) return null;
              const health = source.items[0]?.health;
              const label = source.availability === 'available' ? HEALTH[health ?? 'unknown'] : AVAILABILITY[source.availability];
              return <section className="briefing__item" key={entry.id} data-entry={entry.id}>
                <p className="briefing__eyebrow">中枢来源 · {source.source_label}</p><h2>{label}</h2>
                {source.availability === 'stale' && health ? <p>上次观测：{HEALTH[health]}</p> : null}
                {source.observed_at ? <p className="briefing__time">最近检查 {time(source.observed_at, timezone)}</p> : null}
              </section>;
            })}
        </div>
        <aside className="briefing__summary" aria-label="简报来源">
          <h2>来源</h2><dl>
            <div><dt>日程同步</dt><dd>{projected ? summary(projected.sources.calendar) : '等待读取'}</dd></div>
            <div><dt>家庭提示</dt><dd>{projected ? summary(projected.sources.notice) : '等待读取'}</dd></div>
            <div><dt>中枢来源</dt><dd>{nasSummary}</dd></div>
            <div><dt>房屋资料</dt><dd>{projected ? projected.sources.profile.availability === 'not_connected' ? '尚未填写' : summary(projected.sources.profile) : '等待读取'}</dd></div>
            <div><dt>环境数据</dt><dd>{projected ? summary(projected.sources.environment) : '等待读取'}</dd></div>
          </dl>
          <RemoteButton className="button button--quiet" ref={houseButton} data-return-focus="briefing-house" onClick={() => dispatch({ type: 'router.navigate', route: { name: 'house' }, sourceFocus: 'briefing-house' })} onDirection={key => {
            if (key === 'left' || key === 'up') reading.current?.focus();
            if (key === 'down') refresh.current?.focus();
          }}>查看房屋</RemoteButton>
        </aside>
      </div>
      <div className="briefing__recovery"><RemoteButton className="button" ref={refresh} onClick={() => { void overview.load().catch(() => {}); }} onDirection={key => {
        if (key === 'up') reading.current?.focus();
        if (key === 'down' || key === 'left') focusPrimaryNav('briefing');
      }}>刷新简报</RemoteButton><p role="status">{feedback}</p><span>↑↓ 滚动事项</span></div>
    </main>
    <PrimaryNav onUp={() => reading.current?.focus()} />
  </div>;
}
