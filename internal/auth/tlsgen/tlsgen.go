// Package tlsgen creates the local certificate authority and the server
// certificate used by tls.mode = auto (technical design §6.9).
package tlsgen

import (
	"net"
	"net/url"
	"sort"
	"strings"
	"time"
)

// File names inside <data_dir>/tls.
const (
	CACertFile     = "ca.crt"
	CAKeyFile      = "ca.key"
	ServerCertFile = "server.crt"
	ServerKeyFile  = "server.key"
)

// Lifetimes from the design: ten years for the CA, two for the leaf.
const (
	CALifetime     = 10 * 365 * 24 * time.Hour
	ServerLifetime = 2 * 365 * 24 * time.Hour
)

// Params describes the certificate material to produce.
type Params struct {
	// Dir is the tls directory inside the data dir.
	Dir string
	// PublicURL contributes its host to the SANs.
	PublicURL string
	// ExtraSANs are additional hostnames or IP addresses.
	ExtraSANs []string
	// Now allows tests to pin the validity window.
	Now time.Time
}

// Result reports what Ensure produced.
type Result struct {
	CACertPath     string
	ServerCertPath string
	ServerKeyPath  string
	DNSNames       []string
	IPAddresses    []string
	Created        bool
}

// SANs computes the certificate subject alternative names: the public URL host,
// the extra names from the configuration and the loopback identities.
func SANs(publicURL string, extra []string) (dns []string, ips []net.IP) {
	dnsSet := map[string]struct{}{"localhost": {}}
	ipSet := map[string]net.IP{
		"127.0.0.1": net.ParseIP("127.0.0.1"),
		"::1":       net.ParseIP("::1"),
	}
	add := func(candidate string) {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			return
		}
		if ip := net.ParseIP(candidate); ip != nil {
			ipSet[ip.String()] = ip
			return
		}
		dnsSet[strings.ToLower(candidate)] = struct{}{}
	}
	if publicURL != "" {
		if u, err := url.Parse(publicURL); err == nil && u.Host != "" {
			host := u.Hostname()
			add(host)
		} else {
			add(publicURL)
		}
	}
	for _, e := range extra {
		add(e)
	}
	for name := range dnsSet {
		dns = append(dns, name)
	}
	sort.Strings(dns)
	keys := make([]string, 0, len(ipSet))
	for k := range ipSet {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		ips = append(ips, ipSet[k])
	}
	return dns, ips
}
