package brain

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"

	"github.com/oklog/ulid/v2"
)

// ScopeFor binds a host ledger to the configured HTTPS origin and stable Core
// principal. Credentials deliberately are not arguments: token rotation must
// retain operation ownership. The runtime must obtain the principal from its
// maintainer configuration, never from model-supplied text.
func ScopeFor(coreOrigin, principalID string) (string, error) {
	u, err := url.Parse(coreOrigin)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", errors.New("scope requires an HTTPS Core origin")
	}
	id, err := ulid.ParseStrict(principalID)
	if err != nil || id.String() != principalID {
		return "", errors.New("scope requires a canonical principal ID")
	}
	host := strings.ToLower(u.Hostname())
	if addr, parseErr := netip.ParseAddr(host); parseErr == nil {
		host = addr.String()
	}
	port := u.Port()
	if port != "" {
		n, parseErr := strconv.Atoi(port)
		if parseErr != nil || n < 1 || n > 65535 {
			return "", errors.New("invalid Core port")
		}
		port = strconv.Itoa(n)
	}
	if port != "" && port != "443" {
		host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	sum := sha256.Sum256([]byte("https://" + host + "\x00" + principalID))
	return hex.EncodeToString(sum[:]), nil
}
