export interface ArchiveFilter {
    query?: string;
    entity_id?: string;
    domain?: string;
    ownership?: string;
    cursor?: string;
    limit?: number;
}
export interface ArchiveEvidence {
    field_name?: string;
    asserted_value?: string;
    excerpt: string;
    confirmed: boolean;
    observation_id?: string;
    locator: {
        text?: {
            start: number;
            end: number;
        };
        csv?: {
            record: number;
            column: number;
            physical_row: number;
            byte_column: number;
        };
        xlsx?: {
            sheet_id: string;
            sheet_name: string;
            part_uri: string;
            cell: string;
        };
        docx?: {
            part_uri: string;
            paragraph?: number;
            table_path?: number[];
            row?: number;
            grid_column?: number;
        };
        page?: {
            page: number;
            rect: number[];
        };
    };
    start: number;
    end: number;
    source_kind?: string;
}
export interface ArchiveItem {
 processing?:string;task_summary?:ArchiveTaskSummary;original_saved?:boolean;
    document_id: string;
    document_revision: number;
    domain: string;
    document_type: string;
    ownership: string;
    parse_status: string;
    entity_ids: string[];
    dates: string[];
    evidence: ArchiveEvidence[];
    metadata_only: boolean;
    truncated: boolean;
    review_required: boolean;
}
export interface ArchivePage {
    items: ArchiveItem[];
    next_cursor?: string;
    generation: string;
    coverage: {
        complete: boolean;
        reason?: string;
    };
    index_status: string;
}
export interface ArchiveEntity {
    id: string;
    kind: string;
    names: string[];
    aliases: string[];
}
export interface ArchiveEntities {
    items: ArchiveEntity[];
    next_cursor?: string;
    coverage: {
        complete: boolean;
    };
}
export interface ArchiveTaskSummary {parse:string;understanding:'pending'|'complete'|'needs_review'|'unavailable';attribution:string}
export interface ArchiveDetail {
 task_summary?:ArchiveTaskSummary;
    item: ArchiveItem;
    original_saved: boolean;
    nas_status: string;
    processing: string;
    preview: 'pages' | 'structured' | 'restricted' | 'unsupported';
    generation: string;
    coverage: {
        complete: boolean;
        reason?: string;
    };
}
export interface ArchiveAnalysisQuery {
    ownership?: string;
    operation: 'count' | 'sum' | 'series' | 'latest' | 'ratio';
    count?: 'documents' | 'events' | 'observations';
    metric?: string;
    domain?: string;
    entity_id?: string;
    query?: string;
}
export interface ArchiveAnalysis {
    snapshot_id: string;
    data_epoch: number;
    generation: number;
    algorithm_version: string;
    projection: string;
    groups: {
        metric: string;
        value: string;
        sample?: string;
        unit?: string;
        currency?: string;
        date?: string;
        date_precision?: string;
        denominator?: string;
        numerator?: string;
        rounding?: string;
        change_percent_points?: string;
    }[];
    coverage: {
        complete: boolean;
        included: number;
        excluded: number;
        reasons: string[];
    };
}
