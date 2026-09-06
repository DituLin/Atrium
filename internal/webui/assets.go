package webui

import (
	"io/fs"
	"path"
	"regexp"
	"strings"
)

// hashedName matches the Vite output pattern `name-<hash>.ext`, where the hash
// is at least eight base62 characters. Only those files may be cached forever.
var hashedName = regexp.MustCompile(`-[A-Za-z0-9_]{8,}\.[A-Za-z0-9]+$`)

func isHashedAsset(name string) bool {
	base := path.Base(name)
	if base == IndexFile {
		return false
	}
	return hashedName.MatchString(base)
}

// assetExtensions are paths that must 404 rather than fall back to the SPA
// document, so a missing script is a visible error instead of an HTML body.
var assetExtensions = map[string]struct{}{
	".js": {}, ".mjs": {}, ".css": {}, ".map": {}, ".json": {},
	".png": {}, ".jpg": {}, ".jpeg": {}, ".gif": {}, ".svg": {}, ".webp": {}, ".avif": {},
	".ico": {}, ".woff": {}, ".woff2": {}, ".ttf": {}, ".otf": {}, ".txt": {}, ".wasm": {},
}

func hasAssetExtension(name string) bool {
	ext := strings.ToLower(path.Ext(name))
	_, ok := assetExtensions[ext]
	return ok
}

// Sub returns the dist subtree of an embedded filesystem.
func Sub(embedded fs.FS, dir string) (fs.FS, error) {
	return fs.Sub(embedded, dir)
}

// notBuiltPage explains how to produce the bundle. It uses no external assets.
const notBuiltPage = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Atrium — web UI not built</title>
<style>
  :root { color-scheme: light dark; }
  body { font-family: system-ui, -apple-system, "Segoe UI", Roboto, "Noto Sans", sans-serif;
         margin: 0; display: grid; place-items: center; min-height: 100vh; padding: 2rem; }
  main { max-width: 40rem; }
  h1 { font-size: 1.5rem; margin: 0 0 0.5rem; }
  p { line-height: 1.6; margin: 0 0 1rem; }
  code { background: rgba(127,127,127,0.18); padding: 0.15em 0.4em; border-radius: 4px; }
</style>
</head>
<body>
<main>
  <h1>The Atrium web UI is not built</h1>
  <p>The server is running and the API is available, but no client bundle is
     embedded in this binary.</p>
  <p>Build it with <code>make web</code> and then <code>make build</code>, or run
     the Vite dev server from <code>web/</code> during development.</p>
  <p>The API itself is reachable at <code>/api/v1</code>; liveness is at
     <code>/health/live</code>.</p>
</main>
</body>
</html>
`
