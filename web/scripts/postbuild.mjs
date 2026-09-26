/**
 * Post-build steps.
 *
 * 1. Recreates `dist/.gitkeep`: the Go build embeds `web/dist`, and
 *    `vite build --emptyOutDir` removes the tracked placeholder.
 * 2. Guards the Chrome 66 baseline (TCL TV WebView): CSS features that engine
 *    drops silently must never reach the bundle, whatever the source says.
 */
import { mkdirSync, readdirSync, readFileSync, writeFileSync } from 'node:fs';

mkdirSync('dist', { recursive: true });
writeFileSync('dist/.gitkeep', '');

const UNSUPPORTED = [
  [/(^|[;{])inset(-block|-inline)?:/, '`inset` (use top/right/bottom/left)'],
  [/[:(\s,](min|max|clamp)\(/, 'min()/max()/clamp()'],
];
/** Drops `@supports (...) { ... }` blocks: features behind a support query are progressive enhancement. */
function withoutSupportsBlocks(css) {
  let out = '';
  let index = 0;
  while (index < css.length) {
    const start = css.indexOf('@supports', index);
    if (start === -1) { out += css.slice(index); break; }
    out += css.slice(index, start);
    let depth = 0;
    let cursor = css.indexOf('{', start);
    for (; cursor < css.length; cursor++) {
      if (css[cursor] === '{') depth++;
      else if (css[cursor] === '}' && --depth === 0) break;
    }
    index = cursor + 1;
  }
  return out;
}

const problems = [];
for (const file of readdirSync('dist/assets').filter(name => name.endsWith('.css'))) {
  const css = withoutSupportsBlocks(readFileSync(`dist/assets/${file}`, 'utf8'));
  for (const [pattern, label] of UNSUPPORTED) {
    const match = css.match(pattern);
    if (match) problems.push(`${file}: ${label} near "${css.slice(Math.max(0, match.index - 40), match.index + 40)}"`);
  }
}
if (problems.length) {
  console.error('Chrome 66 CSS guard failed:\n' + problems.join('\n'));
  process.exit(1);
}
