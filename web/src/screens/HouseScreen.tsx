import type { ReactElement, ReactNode } from 'react';
import { useRef } from 'react';
import { useApp } from '../app/context';
import { houseAvailability } from '../app/house';
import type { ConnectionState } from '../app/connection';
import { serverNow } from '../core/clock';
import type { FamilyAvailability, HouseResponse } from '../types/api';
import { RemoteButton } from '../ui/RemoteButton';
import { TopNav } from '../ui/TopNav';
import type { Tone } from './info/familyDate';
import { availabilityTone, familyClockTime, familyTime } from './info/familyDate';
import { HEALTH } from './info/homeRows';
import { onReadingKey } from './info/readingRegion';
import { HouseDashboard } from './house/HouseDashboard';
import type { HouseDashboardHandle } from './house/HouseDashboard';
import { houseDashboardFromHome } from './house/model';
import { IoTObservations } from './house/IoTObservations';

const SOURCE_STATUS: Partial<Record<FamilyAvailability, string>> = {
  loading: '尚未完成首次检查', failed: '暂时无法读取来源状态', stale: '状态待更新', not_connected: '未接入',
};

interface Card { key: string; label: string; value: string; tone: Tone; detail: ReactNode }

function HubCard({ card }: { card: Card }): ReactElement {
  return <article className={`card info-house__card${card.tone === 'warn' ? ' card--warn' : ''}`} aria-label={card.label}>
    <div className="info-house__label"><span className={`dot dot--${card.tone}`} aria-hidden="true" /><span>{card.label}</span></div>
    <div className="info-house__value serif">{card.value}</div>
    <div className="info-house__detail">{card.detail}</div>
  </article>;
}

function nasCard(source: HouseResponse['nas'][number], now: number, failed: boolean, timezone: string): Card {
  const availability = houseAvailability(source, now, failed);
  const item = source.items.find(item => (!item.valid_from || now >= Date.parse(item.valid_from)) && (!item.valid_until || now < Date.parse(item.valid_until)));
  const canShow = availability === 'available' || availability === 'stale';
  const value = SOURCE_STATUS[availability] ?? (item ? HEALTH[item.health] : '尚未确认');
  const tone: Tone = availability === 'available' ? item?.health === 'online' ? 'ok' : !item || item.health === 'unknown' ? 'none' : 'warn' : availabilityTone(availability);
  return { key: source.source_id, label: `照片来源 · ${source.source_label}`, value, tone, detail: <>
    {availability === 'stale' && item ? <p>上次观测：{HEALTH[item.health]}</p> : null}
    {canShow ? <dl className="info-house__times">
      <div><dt>最近检查</dt><dd>{familyTime(source.observed_at, timezone, true)}</dd></div>
      <div><dt>最近可读</dt><dd>{familyTime(item?.last_success_at ?? null, timezone, true)}</dd></div>
    </dl> : null}
  </> };
}

function screenCard(connection: ConnectionState): Card {
  const online = connection.status === 'online';
  const reconnecting = connection.status === 'reconnecting';
  return { key: 'screen', label: '此屏幕', value: online ? '已连接' : reconnecting ? '正在重连' : '未连接', tone: online ? 'ok' : 'warn',
    detail: online ? '实时连接已建立' : reconnecting ? '实时连接未建立 · 正在重连' : '实时连接未建立' };
}

export function HouseScreen() {
  const { state, house } = useApp();
  const details = useRef<HTMLDivElement>(null);
  const recheck = useRef<HTMLButtonElement>(null);
  const dashboard = useRef<HouseDashboardHandle>(null);
  const { snapshot, status: loadStatus, refreshing } = state.house;
  const now = serverNow(state.clock, state.nowMs);
  const failed = loadStatus === 'stale';
  const timezone = snapshot?.home.timezone ?? 'UTC';
  const feedback = refreshing ? '正在检查…' : loadStatus === 'stale' ? '暂时无法更新 · 显示上次观测'
    : loadStatus === 'failed' ? '暂时无法读取中枢状态' : snapshot ? '已更新状态' : '正在读取中枢状态…';
  const cards: Card[] = [];
  if (snapshot) {
    const core = houseAvailability(snapshot.core, now, failed);
    cards.push({ key: 'core', label: `家庭服务 · ${snapshot.core.source_label}`, tone: availabilityTone(core),
      value: core === 'available' ? '最近可响应' : core === 'stale' ? '状态待更新' : SOURCE_STATUS[core] ?? '尚未确认',
      detail: core === 'stale' ? `上次可响应 · ${familyTime(snapshot.core.observed_at, timezone, true)}` : `响应时间 ${familyTime(snapshot.core.observed_at, timezone, true)}` });
  }
  cards.push(screenCard(state.connection));
  if (snapshot) for (const source of snapshot.nas) cards.push(nasCard(source, now, failed, timezone));
  const attention = cards.filter(card => card.tone === 'warn').length;
  const headline = !snapshot ? (loadStatus === 'failed' ? '暂时无法读取中枢状态' : '正在读取中枢状态…')
    : attention ? `${attention} 项需要留意` : cards.every(card => card.tone === 'ok') ? '中枢与照片来源运行正常' : '暂无需要留意的项目';
  const updated = snapshot ? familyClockTime(snapshot.generated_at, timezone, true) : null;
  return <div className="page house-page">
    <TopNav onDown={() => dashboard.current?.focusFirstRoom()} />
    <HouseDashboard ref={dashboard} model={houseDashboardFromHome(state.home)} timezone={timezone} restoreFocus={state.router.restoreFocus} deviceAvailability={snapshot?.iot?.availability} unmappedDevices={snapshot?.iot?.instances.some(instance => instance.devices.length > 0)} deviceObservations={snapshot?.iot ? <IoTObservations iot={snapshot.iot} stale={failed || state.connection.status !== 'online'} timezone={timezone} onLeave={(element, direction) => {
      if (direction === 'up' || direction === 'left') dashboard.current?.focusBefore(element);
      else dashboard.current?.focusAfter(element);
    }} /> : null} sourceStatus={
      <section className="house-sources" aria-label="中枢与照片来源">
        <div className="house-sources__heading"><h2>中枢与照片来源</h2><span>{headline}</span>
          <RemoteButton className="btn" ref={recheck} data-house-focus="refresh" onClick={() => { void house.load().catch(() => {}); }} onDirection={key => {
            if (key === 'up' || key === 'left' || key === 'down') details.current?.focus();
          }}>重新检查</RemoteButton>
        </div>
        <div className="info-house__reading house-sources__reading" role="region" aria-label="中枢与来源" tabIndex={0} data-house-focus="sources" ref={details} onKeyDown={event => onReadingKey(event, {
          right: () => recheck.current?.focus(), top: () => dashboard.current?.focusBefore(details.current), bottom: () => recheck.current?.focus(),
        })}>
          <div className="info-house__cards">{cards.map(card => <HubCard key={card.key} card={card} />)}</div>
          {snapshot && !snapshot.nas.length ? <p className="info-house__empty muted">{failed ? '状态待更新 · 上次没有可展示的照片来源' : '没有可展示的照片来源'}</p> : null}
        </div>
        <footer className="house-sources__footer"><span>方向键 移动 · 返回 回到首页</span><span role="status">{feedback}{updated ? ` · 更新于 ${updated}` : ''}</span></footer>
      </section>
    } />
  </div>;
}
