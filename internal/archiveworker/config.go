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
)

func (w *Worker) ConfigurationFingerprint() (string, error) {
	artifacts := map[string]string{}
	for name, path := range map[string]string{"native": w.Native.HelperPath, "node": w.Adapter.NodePath, "openclaw": w.Adapter.OpenClawPath, "plugin": w.Adapter.PluginPath, "provider": w.Adapter.ProviderPath} {
		if path == "" {
			artifacts[name] = "unconfigured"
			continue
		}
		digest, e := artifactDigest(path)
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
		if count > 4096 {
			return domain.ErrConflict
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		if !info.Mode().IsRegular() {
			return domain.ErrConflict
		}
		bytes += info.Size()
		if bytes > 128<<20 {
			return domain.ErrConflict
		}
		rel, e := filepath.Rel(path, p)
		if e != nil {
			return e
		}
		h.Write([]byte(rel))
		h.Write([]byte{0})
		f, e := os.Open(p)
		if e != nil {
			return e
		}
		_, e = io.Copy(h, io.LimitReader(f, info.Size()+1))
		f.Close()
		return e
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
