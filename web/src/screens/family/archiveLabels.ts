import type { ArchiveEvidence, ArchiveItem } from '../../types/archives';
import { wallClockParts } from '../../core/clock';
import type { ClockFormatOptions } from '../../core/clock';
import { METRICS } from './FamilyTrend';
export const DOMAINS: Record<string, string> = { health: '健康', education: '教育', finance: '财务', property: '房屋', other: '其他', unknown: '待分类' };
const LABELS: Record<string, string> = { ...METRICS, amount: '金额', volume: '奶量', formula: '配方奶', breast_milk: '母乳', lactulose: '乳果糖', score: '成绩', hemoglobin: '血红蛋白', date: '日期', invoice_number: '单据编号', payment: '付款', warranty_end: '保修截止', documents: '档案', events: '事项', observations: '记录' };
export function factLabel(name: string) { return LABELS[name] ?? (/[\u3400-\u9fff]/.test(name) ? name : '记录内容'); }
export function archiveDate(value: string, clock?: ClockFormatOptions): string {
    if (/^\d{4}$/.test(value))
        return `${value}年`;
    if (/^\d{4}-\d{2}$/.test(value))
        return `${value.slice(0, 4)}年${Number(value.slice(5))}月`;
    if (/^\d{4}-\d{2}-\d{2}$/.test(value))
        return `${value.slice(0, 4)}年${Number(value.slice(5, 7))}月${Number(value.slice(8))}日`;
    if (/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/.test(value)) {
        if (clock) {
            const time = Date.parse(value);
            if (!Number.isFinite(time))
                return '日期未明确';
            const p = wallClockParts(time, clock);
            return `${p.year}年${p.month}月${p.day}日 ${String(p.hour).padStart(2, '0')}:${String(p.minute).padStart(2, '0')}`;
        }
        return `${archiveDate(value.slice(0, 10))} ${value.slice(11, 16)}`;
    }
    return '日期未明确';
}
export function itemTitle(item: ArchiveItem) { const fact = item.evidence.find(e => e.field_name === 'title' || e.field_name === 'name'); return fact?.asserted_value ?? fact?.excerpt ?? `${DOMAINS[item.domain] ?? '家庭'}档案`; }
export function evidenceLocation(e: ArchiveEvidence) { const l = e.locator; if (l.page)
    return `原件第 ${l.page.page} 页`; if (l.xlsx)
    return `${l.xlsx.sheet_name} · ${l.xlsx.cell}`; if (l.csv)
    return `表格第 ${l.csv.record} 行，第 ${l.csv.column} 列`; if (l.docx)
    return `文档${l.docx.paragraph ? `第 ${l.docx.paragraph} 段` : '表格段落'}`; return `文本第 ${e.start + 1}–${e.end} 字`; }
export function ownershipLabel(value: string) { return ({ confirmed: '已归属', household: '家庭共有', waiting: '待确认归属', unassigned: '待归属' } as Record<string, string>)[value] ?? '待归属'; }
