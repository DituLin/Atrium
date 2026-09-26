/**
 * 家里 rows on 今日: one honest line per overview source. Sources that are not
 * connected read 未接入 with a hollow dot; nothing here invents a value.
 */

import { houseAvailability } from '../../app/house';
import type { FamilyAvailability, FamilySourceSnapshot, OverviewSources } from '../../types/api';
import type { Tone } from './familyDate';
import { availabilityTone } from './familyDate';

export const HEALTH = { online: '在线', offline: '离线', degraded: '异常', unknown: '尚未确认' } as const;
export const AVAILABILITY: Record<FamilyAvailability, string> = {
  available: '已接入', stale: '状态待更新', loading: '尚未完成首次读取', failed: '暂时无法读取', not_connected: '未接入',
};

export interface HomeRow { key: string; label: string; value: string; tone: Tone }

function summary<T>(source: FamilySourceSnapshot<T>, availability: FamilyAvailability): string {
  return availability === 'available' ? `${source.items.length} 条有效内容` : AVAILABILITY[availability];
}

/**
 * `sources` is the projected overview (NAS and notices already carry expiry);
 * the other sources are projected here with the same House rule.
 */
export function homeRows(sources: OverviewSources | null, now: number, transportFailed: boolean): HomeRow[] {
  if (!sources) {
    return ['中枢', 'NAS', '房屋资料', '环境数据', '日程同步'].map(label => ({ key: label, label, value: '等待读取', tone: 'none' as const }));
  }
  const rows: HomeRow[] = [];
  const core = houseAvailability(sources.core, now, transportFailed);
  rows.push({ key: 'core', label: '中枢', tone: availabilityTone(core), value: core === 'available' ? '可响应' : core === 'stale' ? '状态待更新 · 上次可响应' : AVAILABILITY[core] });
  if (!sources.nas.length) rows.push({ key: 'nas', label: 'NAS', value: '没有可展示的来源', tone: 'none' });
  for (const source of sources.nas) {
    const health = source.items[0]?.health;
    const available = source.availability === 'available';
    const value = available ? HEALTH[health ?? 'unknown'] : source.availability === 'stale' && health ? `状态待更新 · 上次观测：${HEALTH[health]}` : AVAILABILITY[source.availability];
    const tone: Tone = available ? health === 'online' ? 'ok' : health === 'unknown' || !health ? 'none' : 'warn' : availabilityTone(source.availability);
    rows.push({ key: `nas:${source.source_id}`, label: `NAS · ${source.source_label}`, value, tone });
  }
  const profile = houseAvailability(sources.profile, now, transportFailed);
  rows.push({ key: 'profile', label: '房屋资料', tone: availabilityTone(profile), value: profile === 'not_connected' ? '尚未填写' : summary(sources.profile, profile) });
  const environment = houseAvailability(sources.environment, now, transportFailed);
  rows.push({ key: 'environment', label: '环境数据', tone: availabilityTone(environment), value: summary(sources.environment, environment) });
  const calendar = houseAvailability(sources.calendar, now, transportFailed);
  rows.push({ key: 'calendar', label: '日程同步', tone: availabilityTone(calendar), value: summary(sources.calendar, calendar) });
  return rows;
}
