import { execFileSync } from 'node:child_process';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

import react from '@vitejs/plugin-react';
import { defineConfig } from 'vitest/config';

/**
 * `client_version` = package version + short git SHA when available.
 * A missing or failing git must never break the build (CI archives, tarballs).
 */
function clientVersion(): string {
  const pkgPath = fileURLToPath(new URL('./package.json', import.meta.url));
  const pkg = JSON.parse(readFileSync(pkgPath, 'utf8')) as { version?: string };
  const base = pkg.version ?? '0.0.0';
  try {
    const sha = execFileSync('git', ['rev-parse', '--short=7', 'HEAD'], {
      stdio: ['ignore', 'pipe', 'ignore'],
      encoding: 'utf8',
    }).trim();
    return sha ? `${base}+${sha}` : `${base}+dev`;
  } catch {
    return `${base}+dev`;
  }
}

// The dev proxy points at the mock core by default (npm run dev:mock).
// Set ATRIUM_PROXY_TARGET=https://127.0.0.1:8443 to develop against the real server.
const proxyTarget = process.env.ATRIUM_PROXY_TARGET ?? 'http://127.0.0.1:8788';

export default defineConfig({
  plugins: [react()],
  define: {
    __CLIENT_VERSION__: JSON.stringify(clientVersion()),
  },
  // Root-relative asset URLs (/assets/...) so the Go SPA handler can serve them
  // with immutable caching from any path.
  base: '/',
  build: {
    target: 'es2018',
    outDir: 'dist',
    emptyOutDir: true,
    assetsDir: 'assets',
    cssCodeSplit: false,
    sourcemap: false,
    reportCompressedSize: true,
  },
  server: {
    port: 5173,
    strictPort: false,
    proxy: {
      '/api': { target: proxyTarget, changeOrigin: false, secure: false, ws: true },
      '/health': { target: proxyTarget, changeOrigin: false, secure: false },
    },
  },
  test: {
    environment: 'jsdom',
    globals: false,
    include: ['src/**/*.test.ts', 'src/**/*.test.tsx'],
    setupFiles: ['./src/test/setup.ts'],
  },
});
