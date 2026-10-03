export interface FamilyMember { id: string; name: string }
export type FamilyCategory = 'growth' | 'health' | 'checkup' | 'meal' | 'note';
export interface FamilyMeasurement { metric: string; value: number; unit: string }
export interface FamilyRecord {
 id: string; member_id: string; category: FamilyCategory; occurred_at: string;
 title: string; notes: string; measurements: FamilyMeasurement[]; display_on_tv: boolean;
 created_at: string; exported_at?: string;
 supersedes_id?: string; revision_reason?: string; retracted?: boolean; superseded_by?: string;
}
export interface FamilyRecordPage { records: FamilyRecord[]; next_cursor?: string }
