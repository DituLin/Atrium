package config

import (
	"net/url"
	"strings"
)

// Redacted returns a copy safe to print or log: filesystem paths that could
// disclose the NAS layout are replaced with a stable placeholder.
func (c *Config) Redacted() *Config {
	cp := *c
	cp.Sources = make([]Source, len(c.Sources))
	copy(cp.Sources, c.Sources)
	for i := range cp.Sources {
		cp.Sources[i].Root = "<redacted>"
		if cp.Sources[i].Identity.MarkerFile != "" {
			cp.Sources[i].Identity.MarkerFile = "<redacted>"
		}
	}
	return &cp
}

// DefaultAllowedOrigins derives the origin whitelist from public_url and the
// loopback variants of the listen port.
func (c *Config) DefaultAllowedOrigins() []string {
	if len(c.Server.AllowedOrigins) > 0 {
		return c.Server.AllowedOrigins
	}
	scheme := "https"
	if c.Server.TLS.Mode == TLSOff {
		scheme = "http"
	}
	out := make([]string, 0, 3)
	if c.Server.PublicURL != "" {
		if u, err := url.Parse(c.Server.PublicURL); err == nil && u.Host != "" {
			out = append(out, strings.ToLower(u.Scheme+"://"+u.Host))
		}
	}
	port := c.ListenPort()
	if port != "" {
		out = append(out, scheme+"://localhost:"+port, scheme+"://127.0.0.1:"+port)
	}
	return dedupe(out)
}

// ListenPort extracts the port from server.listen.
func (c *Config) ListenPort() string {
	idx := strings.LastIndex(c.Server.Listen, ":")
	if idx < 0 || idx == len(c.Server.Listen)-1 {
		return ""
	}
	return c.Server.Listen[idx+1:]
}

// LocalBaseURL is the loopback URL the admin CLI defaults to.
func (c *Config) LocalBaseURL() string {
	scheme := "https"
	if c.Server.TLS.Mode == TLSOff {
		scheme = "http"
	}
	port := c.ListenPort()
	if port == "" {
		port = "8443"
	}
	return scheme + "://127.0.0.1:" + port
}

func dedupe(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := in[:0]
	for _, v := range in {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}
