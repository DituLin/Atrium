// Package homemcp exposes the permission-filtered Core contract over MCP stdio.
package homemcp

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config contains connection settings, never the service credential itself.
type Config struct {
	CoreURL          string `yaml:"core_url"`
	CAFile           string `yaml:"ca_file"`
	TokenFile        string `yaml:"token_file"`
	RequestTimeoutMS int    `yaml:"request_timeout_ms"`
}

// LoadConfig reads a strict, single-document configuration.
func LoadConfig(path string) (Config, error) {
	var c Config
	b, e := os.ReadFile(path) // #nosec G304 -- explicit operator-selected local configuration path.
	if e != nil {
		return c, errors.New("cannot read MCP configuration")
	}
	d := yaml.NewDecoder(bytes.NewReader(b))
	d.KnownFields(true)
	if d.Decode(&c) != nil {
		return c, errors.New("invalid MCP configuration")
	}
	var extra any
	if !errors.Is(d.Decode(&extra), io.EOF) {
		return c, errors.New("expected one configuration document")
	}
	return c, c.validate()
}
func (c Config) validate() error {
	u, e := url.Parse(c.CoreURL)
	if e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return errors.New("core_url must be an HTTPS origin")
	}
	if c.CAFile == "" || c.TokenFile == "" {
		return errors.New("CA and service token files are required")
	}
	if c.RequestTimeoutMS != 0 && (c.RequestTimeoutMS < 100 || c.RequestTimeoutMS > 10000) {
		return errors.New("request timeout must be between 100 and 10000 milliseconds")
	}
	return nil
}
func (c Config) client() (*http.Client, string, error) {
	if e := c.validate(); e != nil {
		return nil, "", e
	}
	info, e := os.Lstat(c.TokenFile)
	if e != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		return nil, "", errors.New("service token must be a regular non-symlink file with mode 0600")
	}
	f, e := os.Open(c.TokenFile)
	if e != nil {
		return nil, "", errors.New("cannot open service token")
	}
	defer func() { _ = f.Close() }()
	after, e := f.Stat()
	if e != nil || !os.SameFile(info, after) || after.Mode().Perm() != 0600 {
		return nil, "", errors.New("service token changed while opening")
	}
	b, e := io.ReadAll(io.LimitReader(f, 4097))
	token := strings.TrimSpace(string(b))
	if e != nil || len(b) > 4096 || token == "" || strings.ContainsAny(token, "\r\n\t ") {
		return nil, "", errors.New("invalid service token file")
	}
	ca, e := os.ReadFile(c.CAFile)
	pool := x509.NewCertPool()
	if e != nil || !pool.AppendCertsFromPEM(ca) {
		return nil, "", errors.New("invalid household CA")
	}
	timeout := c.RequestTimeoutMS
	if timeout == 0 {
		timeout = 5000
	}
	tr := &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: pool}, ResponseHeaderTimeout: time.Duration(timeout) * time.Millisecond}
	return &http.Client{Transport: tr, Timeout: time.Duration(timeout) * time.Millisecond, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, token, nil
}
