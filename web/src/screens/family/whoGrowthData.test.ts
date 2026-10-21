import { createHash } from 'node:crypto';
import { describe, expect, it } from 'vitest';
import { WHO_GROWTH_SOURCES, WHO_LMS_SCALED, whoLmsAt } from './whoGrowthData';

describe('official daily WHO LMS parameters', () => {
    it('preserves all four complete source-derived parameter sequences', () => {
        const expected = {
            height: { male: 'b6ff6c391066d8da770d97db0677177f096ac6338661ac4a2bb75abd40cf59d9', female: 'cc4136d6e0e5b92bf2988fd28b66854d224f76b965e40577fd88f0c2b729f268' },
            weight: { male: '5ea0b7379beb9aa5aef8aa1d4648d4f2ed55764dfa720340458559fac5f74acd', female: '92aa32a7a9c6226a755d17e7739a8b66dd4113fc5be8d3bd8fade50a52971c53' },
        };
        for (const metric of ['height', 'weight'] as const) for (const sex of ['male', 'female'] as const) {
            const values = WHO_LMS_SCALED[metric][sex];
            expect(values).toHaveLength(1857 * 3);
            expect(values.every(Number.isSafeInteger)).toBe(true);
            expect(createHash('sha256').update(JSON.stringify(values)).digest('hex')).toBe(expected[metric][sex]);
        }
    });
    it('records original workbook hashes and official download provenance', () => {
        expect(WHO_GROWTH_SOURCES.map(source => source.sha256).sort()).toEqual([
            'c4b1c9029ab9751a5f0888e32f35c7c0287a16d361885cf911ecf23b3f7f6b4f', '6aa2876319449a6b1f4d825848128902114ff53c67b92b86a0c5140846013059',
            'b5b4748c6bfa5230e2eddafa1767629c349178b08d457f400b59422b8bfef86c', 'ee3ae12cb96c6c5541cdf43665c03ce6c984f877859a183a5f6104eb06a49a6e',
        ].sort());
        expect(WHO_GROWTH_SOURCES.every(source => source.url.startsWith('https://cdn.who.int/') && source.retrieved === '2026-10-21')).toBe(true);
    });
    it('matches sex-specific daily anchors at birth and approximately twelve months', () => {
        expect(whoLmsAt('height', 'male', 0)).toEqual([1, 49.8842, 0.03795]);
        expect(whoLmsAt('height', 'female', 0)).toEqual([1, 49.1477, 0.0379]);
        expect(whoLmsAt('weight', 'male', 0)).toEqual([0.3487, 3.3464, 0.14602]);
        expect(whoLmsAt('weight', 'female', 0)).toEqual([0.3809, 3.2322, 0.14171]);
        expect(whoLmsAt('height', 'male', 365)).toEqual([1, 75.7391, 0.03137]);
        expect(whoLmsAt('height', 'female', 365)).toEqual([1, 74.0049, 0.03479]);
        expect(whoLmsAt('weight', 'male', 365)).toEqual([0.0645, 9.646, 0.10925]);
        expect(whoLmsAt('weight', 'female', 365)).toEqual([-0.2022, 8.9462, 0.12267]);
    });
    it('uses exact integer ages only and bounds the standards to day 1856', () => {
        for (const day of [-1, 0.5, 1857, NaN, Infinity]) expect(whoLmsAt('weight', 'male', day)).toBeNull();
        expect(whoLmsAt('height', 'male', 1856)).toEqual([1, 110.4969, 0.04226]);
        expect(whoLmsAt('weight', 'female', 1856)).toEqual([-0.3531, 18.389, 0.14892]);
    });
});
