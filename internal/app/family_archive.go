package app

import (
	"context"
	"github.com/DituLin/Atrium/internal/familyarchive"
	"time"
)

func (r *Runtime) runFamilyArchive(ctx context.Context) {
	c := r.cfg.FamilyArchive
	exporter, err := familyarchive.New(familyarchive.Options{DB: r.db, Root: c.Dir, Identity: c.Identity, AllowLocal: c.AllowLocal, Now: r.now})
	if err != nil {
		r.log.Error("family archive configuration invalid", "component", "family_archive")
		return
	}
	pass := func() {
		n, err := exporter.Export(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			r.log.Warn("family archive pending; will retry", "component", "family_archive")
		} else if n > 0 {
			r.log.Info("family records archived", "component", "family_archive", "count", n)
		}
	}
	pass()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pass()
		}
	}
}
