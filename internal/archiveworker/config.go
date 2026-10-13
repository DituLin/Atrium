package archiveworker

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/DituLin/Atrium/internal/archive"
	"github.com/DituLin/Atrium/internal/domain"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ConfigurationFingerprint hashes bounded trusted artifacts and the extraction policy.
func (w *Worker) ConfigurationFingerprint() (string, error) {
	artifacts := map[string]string{}
	// A provider's peer SDK link may point only at the configured OpenClaw
	// installation. Hash that complete bounded installation, including dependency
	// bytes, rather than ignoring node_modules or trusting a package version.
	runtimeRoot := ""
	if w.Adapter.OpenClawPath != "" {
		entry, err := filepath.EvalSymlinks(w.Adapter.OpenClawPath)
		if err != nil {
			return "", err
		}
		candidate := filepath.Dir(entry)
		raw, err := os.ReadFile(filepath.Join(candidate, "package.json")) // #nosec G304 -- Read only package.json adjacent to the explicitly configured OpenClaw entry point; its name and the bounded artifact digest are checked below.
		var pkg struct {
			Name string `json:"name"`
		}
		if err == nil && json.Unmarshal(raw, &pkg) == nil && pkg.Name == "openclaw" {
			runtimeRoot = candidate
			digest, err := artifactDigestWithPolicy(candidate, "", candidate, 100000, 2<<30)
			if err != nil {
				return "", err
			}
			artifacts["openclaw-runtime"] = digest
		}
	}
	for name, path := range map[string]string{"native": w.Native.HelperPath, "node": w.Adapter.NodePath, "openclaw": w.Adapter.OpenClawPath, "plugin": w.Adapter.PluginPath, "provider": w.Adapter.ProviderPath} {
		if path == "" {
			artifacts[name] = "unconfigured"
			continue
		}
		// Only explicitly configured executable entry points may resolve symlinks.
		// Nested plugin/provider resource symlinks remain rejected by artifactDigest.
		if name == "native" || name == "node" || name == "openclaw" {
			resolved, err := filepath.EvalSymlinks(path)
			if err != nil {
				return "", err
			}
			path = resolved
		}
		var digest string
		var e error
		if name == "provider" && runtimeRoot != "" {
			digest, e = artifactDigestWithPolicy(path, runtimeRoot, "", 4096, 128<<20)
		} else {
			digest, e = artifactDigest(path)
		}
		if e != nil {
			return "", e
		}
		artifacts[name] = digest
	}
	data, e := json.Marshal(struct {
		Parser, Understanding, Resolver, Anchors, Publication string
		Limits                                                archive.ParseLimits
		Artifacts                                             map[string]string
	}{archive.ParserVersion, archive.UnderstandingConfigurationFingerprint(), archive.ResolutionRuleVersion, archive.PublicationAnchorRule, archive.PublicationVersion, archive.DefaultParseLimits(), artifacts})
	if e != nil {
		return "", e
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func artifactDigest(path string) (string, error) {
	return artifactDigestWithPolicy(path, "", "", 4096, 128<<20)
}
func artifactDigestWithPolicy(path, peerRuntime, runtimeRoot string, maxFiles int, maxBytes int64) (string, error) {
	h := sha256.New()
	var bytes int64
	count := 0
	err := filepath.WalkDir(path, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			return nil
		}
		count++
		if count > maxFiles {
			return domain.ErrConflict
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		if info.Mode()&os.ModeSymlink != 0 {
			rel, err := filepath.Rel(path, p)
			if err != nil {
				return err
			}
			resolved, err := filepath.EvalSymlinks(p)
			if err != nil {
				return err
			}
			allowed := peerRuntime != "" && rel == filepath.Join("node_modules", "openclaw") && resolved == peerRuntime
			if runtimeRoot != "" {
				targetRel, err := filepath.Rel(runtimeRoot, resolved)
				if err != nil || targetRel == ".." || strings.HasPrefix(targetRel, ".."+string(filepath.Separator)) || filepath.IsAbs(targetRel) {
					return domain.ErrConflict
				}
				target, err := os.Stat(resolved)
				if err != nil || !target.Mode().IsRegular() {
					return domain.ErrConflict
				}
				// WalkDir hashes the real target independently. Hash the binding too.
				allowed = true
			}
			if !allowed {
				return domain.ErrConflict
			}
			h.Write([]byte("link:" + rel + "\x00" + resolved + "\x00"))
			return nil
		}
		if !info.Mode().IsRegular() {
			return domain.ErrConflict
		}
		bytes += info.Size()
		if bytes > maxBytes {
			return domain.ErrConflict
		}
		rel, e := filepath.Rel(path, p)
		if e != nil {
			return e
		}
		h.Write([]byte(rel))
		h.Write([]byte{0})
		f, e := os.Open(p) // #nosec G304 G122 -- Administrator-configured runtime/plugin trees must remain unchanged during fingerprinting; symlink policy, regular-file/SameFile checks and byte bounds remain enforced.
		if e != nil {
			return e
		}
		opened, e := f.Stat()
		if e != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
			_ = f.Close() // Read-only source cleanup; read errors are handled separately.
			return domain.ErrConflict
		}
		n, e := io.Copy(h, io.LimitReader(f, info.Size()+1))
		_ = f.Close() // Read-only source cleanup; read errors are handled separately.
		if e == nil && n != info.Size() {
			return domain.ErrConflict
		}
		return e
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
