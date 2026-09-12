import type { HouseResponse } from '../types/api';
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
