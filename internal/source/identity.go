package source

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"

	"github.com/DituLin/Atritum/internal/domain"
)

// networkFilesystems is the allowlist from design §6.1. A root that is not on
// one of these is not accepted as a NAS share unless identity.allow_local is
// set, which is what stops an empty local directory left behind by a lost
// mount from being indexed as if it were the library.
var networkFilesystems = map[string]bool{
	"smbfs":  true,
	"nfs":    true,
	"afpfs":  true,
	"webdav": true,
}

// IsNetworkFilesystem reports whether fstype is an accepted network mount.
func IsNetworkFilesystem(fstype string) bool { return networkFilesystems[fstype] }

// IdentityConfig mirrors the per-source identity block of the configuration.
type IdentityConfig struct {
	RequireMount bool
	AllowLocal   bool
	MarkerFile   string
}

// Health detail codes. They are stable strings: the CLI, the API and the
// runbook all match on them, and none of them carries a path or a credential.
const (
	DetailOK               = ""
	DetailRootMissing      = "root_missing"
	DetailNotConnected     = "not_connected"
	DetailPermissionDenied = "permission_denied"
	DetailNotADirectory    = "not_a_directory"
	DetailNotAMount        = "not_a_mount"
	DetailMarkerMissing    = "marker_missing"
	DetailProbeTimeout     = "probe_timeout"
	DetailStuckIO          = "stuck_io"
	DetailIdentityMismatch = "identity_mismatch"
	DetailStatfsFailed     = "statfs_failed"
	DetailNeverProbed      = "never_probed"
)

// Probe is the outcome of one health check.
type Probe struct {
	Health   domain.Health
	Detail   string
	Identity *domain.Identity
	Volume   VolumeStats
	// Err carries the underlying cause for logging at debug level.
	Err error
}

// OK reports whether the probe observed a healthy, identified root.
func (p Probe) OK() bool { return p.Health == domain.HealthOnline }

// HashMountFrom reduces the mount source to a stable, non-reversible token.
// The raw value can embed a user name and a host, so only the hash is stored.
func HashMountFrom(v string) string {
	if v == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(v))
	return hex.EncodeToString(sum[:])[:16]
}

// CheckIdentity runs one probe of a root through fs and classifies the result.
// It performs no database access, so it is trivially testable with FakeFS.
func CheckIdentity(ctx context.Context, f FS, cfg IdentityConfig) Probe {
	info, err := f.Stat(ctx, "")
	if err != nil {
		return classifyProbeError(err)
	}
	if !info.IsDir() {
		return Probe{Health: domain.HealthOffline, Detail: DetailNotADirectory}
	}

	vol, err := f.Statfs(ctx)
	if err != nil {
		if errors.Is(err, ErrStuck) {
			return Probe{Health: domain.HealthUnknown, Detail: DetailProbeTimeout, Err: err}
		}
		return Probe{Health: domain.HealthUnknown, Detail: DetailStatfsFailed, Err: err}
	}

	ident := domain.Identity{
		FSType:         vol.FSType,
		MountFromHash:  HashMountFrom(vol.MountFrom),
		IsNetworkMount: IsNetworkFilesystem(vol.FSType),
	}

	// require_mount asks that the root live on a network filesystem, not that
	// it *be* the mount point: PRD 5.2 authorizes a photo directory, which on
	// a real deployment is a subdirectory of the share (`<mount>/Photos`) and
	// therefore shares its parent's device number. The protection that matters
	// is still in place: a share that failed to mount leaves either nothing
	// (ENOENT -> offline/root_missing) or a local directory on apfs, which is
	// rejected here as not_a_mount.
	if cfg.RequireMount && !cfg.AllowLocal && !ident.IsNetworkMount {
		return Probe{Health: domain.HealthUnknown, Detail: DetailNotAMount, Volume: vol}
	}

	if cfg.MarkerFile != "" {
		if _, merr := f.Stat(ctx, cfg.MarkerFile); merr != nil {
			if errors.Is(merr, os.ErrNotExist) {
				return Probe{Health: domain.HealthUnknown, Detail: DetailMarkerMissing, Volume: vol}
			}
			p := classifyProbeError(merr)
			p.Volume = vol
			return p
		}
		ident.Marker = true
	}

	return Probe{Health: domain.HealthOnline, Detail: DetailOK, Identity: &ident, Volume: vol}
}

// classifyProbeError maps an I/O failure to a health state (design §6.1).
func classifyProbeError(err error) Probe {
	switch {
	case errors.Is(err, ErrStuck):
		return Probe{Health: domain.HealthUnknown, Detail: DetailProbeTimeout, Err: err}
	case errors.Is(err, ErrDegraded):
		return Probe{Health: domain.HealthDegraded, Detail: DetailStuckIO, Err: err}
	case errors.Is(err, os.ErrNotExist):
		return Probe{Health: domain.HealthOffline, Detail: DetailRootMissing, Err: err}
	case errors.Is(err, os.ErrPermission):
		return Probe{Health: domain.HealthDegraded, Detail: DetailPermissionDenied, Err: err}
	case isNotConnected(err):
		return Probe{Health: domain.HealthOffline, Detail: DetailNotConnected, Err: err}
	case errors.Is(err, ErrSymlink), errors.Is(err, ErrUnsafePath):
		return Probe{Health: domain.HealthOffline, Detail: DetailNotADirectory, Err: err}
	default:
		var pathErr *fs.PathError
		if errors.As(err, &pathErr) {
			return Probe{Health: domain.HealthOffline, Detail: DetailRootMissing, Err: err}
		}
		return Probe{Health: domain.HealthUnknown, Detail: DetailStatfsFailed, Err: err}
	}
}
