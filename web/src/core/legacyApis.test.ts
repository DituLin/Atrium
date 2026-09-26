import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join, relative } from 'node:path';
import { expect, it } from 'vitest';

// The TCL TV runs Chrome 66. Syntax is lowered by the build, but built-in APIs
// are not polyfilled: a call to one of these crashes the page on the TV only.
const MISSING_IN_CHROME_66: readonly [RegExp, string][] = [
  [/\.flatMap\(/, 'Array.prototype.flatMap (69)'],
  [/\.flat\(/, 'Array.prototype.flat (69)'],
  [/Object\.fromEntries\(/, 'Object.fromEntries (73)'],
  [/\.replaceAll\(/, 'String.prototype.replaceAll (85)'],
  [/Promise\.(allSettled|any)\(/, 'Promise.allSettled/any (76/85)'],
  [/\.(findLast|findLastIndex|toSorted|toReversed|toSpliced)\(/, 'ES2023 array methods'],
  [/[\w)\]]\.at\(/, 'Array/String.prototype.at (92)'],
  [/Object\.hasOwn\(/, 'Object.hasOwn (93)'],
  [/structuredClone\(/, 'structuredClone (98)'],
  [/\.matchAll\(/, 'String.prototype.matchAll (73)'],
  [/queueMicrotask\(/, 'queueMicrotask (71)'],
  [/AbortSignal\.timeout\(/, 'AbortSignal.timeout (103)'],
  [/\bglobalThis\b/, 'globalThis (71)'],
];

function sources(dir: string): string[] {
  return readdirSync(dir).flatMap(name => {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) return name === 'test' ? [] : sources(path);
    return /\.(ts|tsx)$/.test(name) && !/\.test\.tsx?$/.test(name) ? [path] : [];
  });
}

it('uses no built-in API that Chrome 66 lacks in application code', () => {
  const root = join(__dirname, '..');
  const hits: string[] = [];
  for (const file of sources(root)) {
    readFileSync(file, 'utf8').split('\n').forEach((line, index) => {
      if (/^\s*(\/\/|\*)/.test(line)) return;
      for (const [pattern, label] of MISSING_IN_CHROME_66) {
        if (pattern.test(line)) hits.push(`${relative(root, file)}:${index + 1} ${label}`);
      }
    });
  }
  expect(hits).toEqual([]);
});
