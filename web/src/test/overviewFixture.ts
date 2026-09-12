import type { OverviewResponse } from '../types/api';
import { houseFixture } from './houseFixture';
export function overviewFixture(): OverviewResponse {
 const { schema_version, generated_at, home, ...sources } = houseFixture();
 return { schema_version, generated_at, home, sources: { ...sources,
 notice: { source_id: 'notice', source_label: '家庭提示', observed_at: generated_at, expires_at: null, availability: 'available', reason: null,
 items: [{ id: 'notice', updated_at: null, valid_from: null, valid_until: null, text: '测试提示正文' }] },
 calendar: { source_id: 'calendar', source_label: '家庭日历', observed_at: null, expires_at: null, availability: 'not_connected', reason: 'not_configured', items: [] } }, entries: [] };
}
