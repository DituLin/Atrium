// Package webui serves the embedded single-page client.
package webui

import (
	"errors"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// IndexFile is the SPA entry document.
const IndexFile = "index.html"

// Cache-Control values from design §7.5: hashed assets are immutable, the
// entry document must always be revalidated.
const (
	immutableCache = "public, max-age=31536000, immutable"
	noCache        = "no-cache, must-revalidate"
)

// Handler serves the embedded client with SPA fallback.
type Handler struct {
	files    fs.FS
	hasIndex bool
	// apiPrefixes are never rewritten to index.html.
	apiPrefixes []string
}

// Options configures the handler.
type Options struct {
	// FS is the built bundle, rooted at the directory holding index.html.
	FS fs.FS
	// APIPrefixes must return 404 instead of the SPA document.
	APIPrefixes []string
}

// New builds the SPA handler. A missing index.html is not an error: the
// handler then renders a short "UI not built" page.
func New(opts Options) *Handler {
	prefixes := opts.APIPrefixes
	if len(prefixes) == 0 {
		prefixes = []string{"/api/", "/health/"}
	}
	h := &Handler{files: opts.FS, apiPrefixes: prefixes}
	if opts.FS != nil {
		if _, err := fs.Stat(opts.FS, IndexFile); err == nil {
			h.hasIndex = true
		}
	}
	return h
}

// HasIndex reports whether a built bundle is embedded.
func (h *Handler) HasIndex() bool { return h.hasIndex }

// ServeHTTP serves an asset, or the SPA document for a client route.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	for _, p := range h.apiPrefixes {
		if strings.HasPrefix(r.URL.Path, p) {
			http.NotFound(w, r)
			return
		}
	}
	if !h.hasIndex {
		h.serveNotBuilt(w)
		return
	}

	name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if name == "" || name == "." {
		h.serveIndex(w, r)
		return
	}
	f, err := h.files.Open(name)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			// Unknown path with an asset extension is a genuine miss; anything
			// else is a client route and gets the SPA document.
			if hasAssetExtension(name) {
				http.NotFound(w, r)
				return
			}
			h.serveIndex(w, r)
			return
		}
		http.Error(w, "cannot read asset", http.StatusInternalServerError)
		return
	}
	defer func() { _ = f.Close() }()

	st, err := f.Stat()
	if err != nil || st.IsDir() {
		h.serveIndex(w, r)
		return
	}
	rs, ok := f.(io.ReadSeeker)
	if !ok {
		http.Error(w, "cannot read asset", http.StatusInternalServerError)
		return
	}
	if isHashedAsset(name) {
		w.Header().Set("Cache-Control", immutableCache)
	} else {
		w.Header().Set("Cache-Control", noCache)
	}
	http.ServeContent(w, r, st.Name(), st.ModTime(), rs)
}

func (h *Handler) serveIndex(w http.ResponseWriter, r *http.Request) {
	raw, err := fs.ReadFile(h.files, IndexFile)
	if err != nil {
		h.serveNotBuilt(w)
		return
	}
	w.Header().Set("Cache-Control", noCache)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	_, _ = w.Write(raw)
}

func (h *Handler) serveNotBuilt(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", noCache)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = w.Write([]byte(notBuiltPage))
}
