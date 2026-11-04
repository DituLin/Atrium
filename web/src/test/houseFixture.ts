import type { HouseIoT, HouseResponse } from '../types/api';
export function houseFixture(): HouseResponse {
  const observed_at = '2026-09-12T08:00:00Z';
  const expires_at = '2026-09-12T08:01:00Z';
  const meta = { id: 'health', updated_at: null, valid_from: null, valid_until: null };
  const base = { observed_at, expires_at, availability: 'available' as const, reason: null };
  const missing = { observed_at: null, expires_at: null, availability: 'not_connected' as const, items: [] };
  return { schema_version: 1, generated_at: observed_at, home: { name: '测试家庭', timezone: 'Asia/Singapore' },
    core: { ...base, source_id: 'core', source_label: 'Core', items: [{ ...meta, id: 'response', responding: true }] },
    nas: [{ ...base, source_id: 'photos', source_label: '家庭照片', items: [{ ...meta, health: 'offline', last_success_at: '2026-09-11T08:00:00Z' }] }],
    profile: { ...missing, source_id: 'profile', source_label: '房屋资料', reason: 'not_provided' },
    environment: { ...missing, source_id: 'environment', source_label: '环境数据', reason: 'not_supported' } };
}

export function iotFixture(syncedAt = '2026-09-12T08:00:00Z'): HouseIoT {
  const observation = { value: null as boolean | number | 'off' | null, availability: 'available' as const, source_updated_at: null, sampled_at: null, received_at: syncedAt, confirmed_at: syncedAt };
  return { availability: 'available' as const, reason: null, instances: [{ id: 'demo', provider: 'home_assistant' as const, status: 'connected' as const, generation: 1, last_success_at: observation.received_at, devices: [
    { id: 'humidifier', name: '示例加湿器', room_id: 'living', room_name: '示例客厅', capabilities: [{ id: 'power', name: '电源', kind: 'power' as const, observation: { ...observation, value: false } }, { id: 'humidity', name: '湿度', kind: 'humidity' as const, observation: { ...observation, value: 0, unit: '%' } }] },
    { id: 'thermometer', name: '示例温湿度计', room_id: '', room_name: '', capabilities: [{ id: 'temperature', name: '温度', kind: 'temperature' as const, observation: { ...observation, availability: 'unknown' as const, unit: '°C' } }] },
    { id: 'aquarium', name: '示例鱼缸', room_id: 'living', room_name: '示例客厅', capabilities: [{ id: 'state', name: '运行状态', kind: 'operating_state' as const, observation: { ...observation, availability: 'unavailable' as const } }] },
  ] }] };
}
