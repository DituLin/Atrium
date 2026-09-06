// Package auth owns credentials: token generation and hashing, pairing,
// request authentication, origin checks and rate limits.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Token prefixes (technical design §6.8).
const (
	ScreenPrefix = "atr_scr_"
	AdminPrefix  = "atr_adm_"
	// SecretBytes is the entropy behind every token: 32 bytes rendered as 43
	// base64url characters without padding.
	SecretBytes = 32
	// SecretChars is the length of the encoded random part.
	SecretChars = 43
)

// AdminTokenFile is the file `atrium init` writes inside the data dir.
const AdminTokenFile = "admin.token"

// Scope is an authorization level.
type Scope string

// Scopes.
const (
	ScopeNone   Scope = ""
	ScopeScreen Scope = "screen"
	ScopeAdmin  Scope = "admin"
)

// NewScreenToken returns a fresh screen credential.
func NewScreenToken() (string, error) { return newToken(ScreenPrefix) }

// NewAdminToken returns a fresh admin credential.
func NewAdminToken() (string, error) { return newToken(AdminPrefix) }

func newToken(prefix string) (string, error) {
	buf := make([]byte, SecretBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("auth: read random bytes: %w", err)
	}
	return prefix + base64.RawURLEncoding.EncodeToString(buf), nil
}

// HashToken returns the lower-case hex sha256 of a token. Only the hash is
// stored, so credential comparison is a constant-time index lookup.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// ScopeOf reports the scope a well-formed token grants, or ScopeNone.
func ScopeOf(token string) Scope {
	switch {
	case ValidToken(token, ScreenPrefix):
		return ScopeScreen
	case ValidToken(token, AdminPrefix):
		return ScopeAdmin
	default:
		return ScopeNone
	}
}

// ValidToken reports whether token has the given prefix and a well-formed
// secret. Format checking keeps malformed input out of the database lookup.
func ValidToken(token, prefix string) bool {
	if !strings.HasPrefix(token, prefix) {
		return false
	}
	secret := token[len(prefix):]
	if len(secret) != SecretChars {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(secret)
	return err == nil && len(raw) == SecretBytes
}

// WriteAdminTokenFile stores the admin token at mode 0600 inside dir.
func WriteAdminTokenFile(dir, token string) (string, error) {
	path := filepath.Join(dir, AdminTokenFile)
	if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("auth: write admin token file: %w", err)
	}
	// WriteFile respects the umask for an existing file; force the mode.
	if err := os.Chmod(path, 0o600); err != nil {
		return "", fmt.Errorf("auth: set admin token file mode: %w", err)
	}
	return path, nil
}

// ReadAdminTokenFile reads and trims the admin token file inside dir.
func ReadAdminTokenFile(dir string) (string, error) {
	path := filepath.Join(dir, AdminTokenFile)
	raw, err := os.ReadFile(path) //nolint:gosec // path is inside the data dir
	if err != nil {
		return "", fmt.Errorf("auth: read admin token file: %w", err)
	}
	return strings.TrimSpace(string(raw)), nil
}

// EnvAdminTokenName is the environment variable the CLI reads for a token.
const EnvAdminTokenName = "ATRIUM_ADMIN_TOKEN"
