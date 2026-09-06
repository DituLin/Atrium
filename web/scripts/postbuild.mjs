/**
 * Recreates `dist/.gitkeep` after every build.
 *
 * The Go build embeds `web/dist` with `//go:embed all:dist`, which fails if the
 * directory does not exist. `.gitkeep` is the backend track's tracked file;
 * `vite build --emptyOutDir` removes it, so it is restored here.
 */
import { mkdirSync, writeFileSync } from 'node:fs';

mkdirSync('dist', { recursive: true });
writeFileSync('dist/.gitkeep', '');
