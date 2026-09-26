import type { ReactElement, ReactNode } from 'react';
import { useEffect, useRef } from 'react';
import { useApp } from '../app/context';
import { houseAvailability } from '../app/house';
import type { ConnectionState } from '../app/connection';
import { serverNow } from '../core/clock';
import type { FamilyAvailability, HouseResponse } from '../types/api';
import { RemoteButton } from '../ui/RemoteButton';
import { TopNav, focusTopNav } from '../ui/TopNav';
import { StatusRow } from './info/StatusRow';
import type { Tone } from './info/familyDate';
import { availabilityTone, familyClockTime, familyTime } from './info/familyDate';
import { HEALTH } from './info/homeRows';
import { onReadingKey, restoreReturnFocus } from './info/readingRegion';

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
  useEffect(() => { restoreReturnFocus(state.router.restoreFocus, details.current); }, [state.router.restoreFocus]);
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
  const profile = snapshot ? houseAvailability(snapshot.profile, now, failed) : null;
  const environment = snapshot ? houseAvailability(snapshot.environment, now, failed) : null;
  const updated = snapshot ? familyClockTime(snapshot.generated_at, timezone, true) : null;
  return <div className="page info-house">
    <TopNav onDown={() => details.current?.focus()} />
    <div className="page__heading">
      <h1 className="page__title">房屋</h1>
      <span className="info-house__headline muted">{headline}</span>
    </div>
    <main className="page__body info-house__grid">
      <section className="info-house__hub">
        <h2 className="page__section-title">中枢</h2>
        <div className="info-house__reading" role="region" aria-label="中枢与来源" tabIndex={0} ref={details} onKeyDown={event => onReadingKey(event, {
          right: () => recheck.current?.focus(), top: () => focusTopNav(), bottom: () => recheck.current?.focus(),
        })}>
          <div className="info-house__cards">{cards.map(card => <HubCard key={card.key} card={card} />)}</div>
          {snapshot && !snapshot.nas.length ? <p className="info-house__empty muted">{failed ? '状态待更新 · 上次没有可展示的照片来源' : '没有可展示的照片来源'}</p> : null}
        </div>
      </section>
      <aside className="info-house__side">
        <h2 className="page__section-title">房间与资料</h2>
        <div className="info-house__profile">
          <svg className="info-house__icon" viewBox="0 0 24 24" aria-hidden="true"><path d="M3 11l9-7 9 7 M5 10v10h14V10 M10 20v-6h4v6" /></svg>
          {profile === 'available' ? <div className="info-house__profile-title serif">房屋资料已接入 · {snapshot!.profile.items.length} 条有效内容</div>
            : <div className="info-house__profile-title serif">{profile === 'not_connected' ? '房屋资料尚未填写' : profile ? `房屋资料${SOURCE_STATUS[profile] ?? ''}` : '房屋资料等待读取'}</div>}
          <p className="info-house__note">公开房屋资料待提供。这里只显示允许在客厅屏幕出现的内容。</p>
        </div>
        <StatusRow tone={environment ? availabilityTone(environment) : 'none'} label="环境数据"
          value={environment === 'available' ? `${snapshot!.environment.items.length} 条有效内容` : environment ? SOURCE_STATUS[environment] ?? '尚未确认' : '等待读取'} />
        <div className="info-actions">
          <RemoteButton className="btn" ref={recheck} onClick={() => { void house.load().catch(() => {}); }} onDirection={key => {
            if (key === 'up' || key === 'left') details.current?.focus();
          }}>重新检查</RemoteButton>
        </div>
      </aside>
    </main>
    <footer className="hints">
      <span>方向键 移动</span><span>↑↓ 滚动详情</span><span>返回 回到首页</span>
      <span className="hints__end"><span role="status">{feedback}</span>{updated ? <span> · 更新于 {updated}</span> : null}</span>
    </footer>
  </div>;
}
