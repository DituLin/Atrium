export interface PlotPoint { x: number; y: number }
interface Segment { points: PlotPoint[]; line: string; fill: string }

/** Shape-preserving cubic Hermite interpolation. Duplicate coordinates and
 * missing observations start another run; they never create a fake slope. */
export function trendSegments(points: readonly (PlotPoint | null)[], baseline: number): Segment[] {
    const runs: PlotPoint[][] = [];
    let run: PlotPoint[] = [];
    for (const point of points) {
        if (!point || !Number.isFinite(point.x) || !Number.isFinite(point.y)) {
            if (run.length) runs.push(run);
            run = [];
            continue;
        }
        if (run.length && point.x <= run[run.length - 1]!.x) {
            runs.push(run);
            run = [];
        }
        run.push(point);
    }
    if (run.length) runs.push(run);
    return runs.map(points => {
        const first = points[0]!, last = points[points.length - 1]!;
        const widths = points.slice(1).map((p, i) => p.x - points[i]!.x);
        const slopes = widths.map((width, i) => (points[i + 1]!.y - points[i]!.y) / width);
        const tangents = points.map((_, i) => {
            if (!i) return slopes[0] ?? 0;
            if (i === points.length - 1) return slopes[i - 1]!;
            const previous = slopes[i - 1]!, next = slopes[i]!;
            if (previous * next <= 0) return 0;
            const a = 2 * widths[i]! + widths[i - 1]!, b = widths[i]! + 2 * widths[i - 1]!;
            return (a + b) / (a / previous + b / next);
        });
        for (let i = 0; i < slopes.length; i++) {
            const slope = slopes[i]!;
            if (!slope) { tangents[i] = 0; tangents[i + 1] = 0; continue; }
            const magnitude = Math.hypot(tangents[i]! / slope, tangents[i + 1]! / slope);
            if (magnitude > 3) {
                tangents[i] = 3 / magnitude * tangents[i]!;
                tangents[i + 1] = 3 / magnitude * tangents[i + 1]!;
            }
        }
        let line = `M ${first.x} ${first.y}`;
        const linear = slopes.some(s => !Number.isFinite(s)) || tangents.some(t => !Number.isFinite(t));
        for (let i = 0; i < widths.length; i++) {
            const a = points[i]!, b = points[i + 1]!, third = widths[i]! / 3;
            line += linear ? ` L ${b.x} ${b.y}` : ` C ${a.x + third} ${a.y + third * tangents[i]!} ${b.x - third} ${b.y - third * tangents[i + 1]!} ${b.x} ${b.y}`;
        }
        return { points, line, fill: points.length > 1 && Number.isFinite(baseline) ? `${line} L ${last.x} ${baseline} L ${first.x} ${baseline} Z` : '' };
    });
}
