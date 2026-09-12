import { useEffect, useRef } from 'react';
import { useApp } from '../app/context';
import { houseAvailability } from '../app/house';
import { serverNow } from '../core/clock';
import type { FamilySourceSnapshot, HouseResponse } from '../types/api';
import { Masthead, PrimaryNav, focusPrimaryNav } from '../ui/PrimaryNav';
import { RemoteButton } from '../ui/RemoteButton';
import { mapRemoteKey } from '../ui/keys';

const HEALTH = { online: '在线', offline: '离线', degraded: '异常', unknown: '尚未确认' };
function time(value: string | null, timezone: string): string {
  if (!value) return '尚无记录';
  try { return new Intl.DateTimeFormat('zh-CN', { timeZone: timezone, month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false }).format(new Date(value)); }
  catch { return value; }
}
function status<T>(source: FamilySourceSnapshot<T>, now: number, failed: boolean): string {
  switch (houseAvailability(source, now, failed)) {
    case 'loading': return '尚未完成首次检查';
    case 'failed': return '暂时无法读取来源状态';
    case 'stale': return '状态待更新';
    case 'not_connected': return '未接入';
    default: return '';
  }
}
function NasSource({ source, now, failed, timezone }: { source: HouseResponse['nas'][number]; now: number; failed: boolean; timezone: string }) {
  const availability = houseAvailability(source, now, failed);
  const item = source.items.find(item => (!item.valid_from || now >= Date.parse(item.valid_from)) && (!item.valid_until || now < Date.parse(item.valid_until)));
  const canShow = availability === 'available' || availability === 'stale';
  return <section className="house__source" aria-label={source.source_label}>
    <h3>{source.source_label}</h3>
    <p className={`house__health house__health--${canShow && item ? item.health : 'unknown'}`}>
      {status(source, now, failed) || (item ? HEALTH[item.health] : '尚未确认')}
      {availability === 'stale' && item ? <span> · 上次观测：{HEALTH[item.health]}</span> : null}
    </p>
    {canShow ? <dl className="house__times">
      <div><dt>最近检查</dt><dd>{time(source.observed_at, timezone)}</dd></div>
      <div><dt>最近可读</dt><dd>{time(item?.last_success_at ?? null, timezone)}</dd></div>
    </dl> : null}
  </section>;
}
export function HouseScreen() {
  const { state, house } = useApp();
  const details = useRef<HTMLDivElement>(null);
  const recheck = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    const source = state.router.restoreFocus
      ? Array.from(document.querySelectorAll<HTMLElement>('[data-return-focus]')).find(element => element.dataset.returnFocus === state.router.restoreFocus) : null;
    (source ?? details.current)?.focus();
  }, [state.router.restoreFocus]);
  const { snapshot, status: loadStatus, refreshing } = state.house;
  const now = serverNow(state.clock, state.nowMs);
  const failed = loadStatus === 'stale';
  const timezone = snapshot?.home.timezone ?? 'UTC';
  const coreStale = snapshot && houseAvailability(snapshot.core, now, failed) === 'stale';
  const feedback = refreshing ? '正在检查…' : loadStatus === 'stale' ? '暂时无法更新 · 显示上次观测'
    : loadStatus === 'failed' ? '暂时无法读取中枢状态' : snapshot ? '已更新状态' : '正在读取中枢状态…';
  return <div className="screen screen--house">
    <Masthead /><h1 className="photos__title">房屋</h1>
    <main className="house__main">
      <div className="house__columns">
      <div className="house__details" role="region" aria-label="中枢与来源" tabIndex={0} ref={details} onKeyDown={event => {
        const key = mapRemoteKey(event);
        if (!key || key === 'back' || key === 'enter') return;
        event.preventDefault(); event.stopPropagation();
        const element = event.currentTarget;
        if (key === 'left') focusPrimaryNav('house');
        if (key === 'right') recheck.current?.focus();
        if (key === 'up') element.scrollTop = Math.max(0, element.scrollTop - element.clientHeight * .7);
        if (key === 'down') {
          if (element.scrollTop + element.clientHeight >= element.scrollHeight - 1) recheck.current?.focus();
          else element.scrollTop += element.clientHeight * .7;
        }
      }}>
          <section className="house__hub">
            <h2>中枢与来源</h2>
            <dl className="house__summary">
              <div><dt>Core</dt><dd>{!snapshot ? (loadStatus === 'failed' ? '暂时无法读取中枢状态' : '正在读取中枢状态…')
                : <>{coreStale ? '状态待更新 · 上次可响应' : '最近可响应'}<span className="house__note"> · 响应时间 {time(snapshot.core.observed_at, timezone)}</span></>}</dd></div>
              <div><dt>此屏幕</dt><dd>{state.connection.status === 'online' ? '实时连接已建立' : state.connection.status === 'reconnecting' ? '实时连接未建立 · 正在重连' : '实时连接未建立'}</dd></div>
            </dl>
            <h3 className="house__sources-title">授权照片来源</h3>
            {snapshot ? snapshot.nas.length ? snapshot.nas.map(source => <NasSource key={source.source_id} source={source} now={now} failed={failed} timezone={timezone} />)
              : <p>{failed ? '状态待更新 · 上次没有可展示的照片来源' : '没有可展示的照片来源'}</p> : null}
          </section>
      </div>
          <aside className="house__placeholders">
            <section><h2>房屋资料</h2><p>尚未填写</p><p className="house__note">公开房屋资料待提供。</p></section>
            <section><h2>环境数据</h2><p>未接入</p><p className="house__note">尚未连接环境传感器。</p></section>
          </aside>
      </div>
      <div className="house__recovery">
        <RemoteButton className="button" ref={recheck} onClick={() => { void house.load().catch(() => {}); }} onDirection={key => {
          if (key === 'up') details.current?.focus();
          if (key === 'down' || key === 'left') focusPrimaryNav('house');
        }}>重新检查</RemoteButton>
        <p role="status">{feedback}</p><span className="house__scroll-hint">↑↓ 滚动详情</span>
      </div>
    </main>
    <PrimaryNav onUp={() => details.current?.focus()} />
  </div>;
}
