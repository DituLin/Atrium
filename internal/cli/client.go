package cli

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/DituLin/Atritum/internal/auth"
	"github.com/DituLin/Atritum/internal/domain"
)

// Client talks to a running server over loopback with the admin token.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// ClientOptions configures the admin client.
type ClientOptions struct {
	BaseURL string
	Token   string
	// CAFile is the local CA the server presents; it is trusted automatically.
	CAFile  string
	Timeout time.Duration
}

// NewClient builds an admin API client that trusts the local CA.
func NewClient(opts ClientOptions) (*Client, error) {
	if opts.BaseURL == "" {
		return nil, errors.New("cli: --url is required")
	}
	if opts.Token == "" {
		return nil, errors.New("cli: no admin token; pass --token, set ATRIUM_ADMIN_TOKEN, or use --data-dir")
	}
	if opts.Timeout == 0 {
		opts.Timeout = 15 * time.Second
	}
	transport := &http.Transport{}
	if opts.CAFile != "" {
		pem, err := os.ReadFile(opts.CAFile)
		if err == nil {
			pool := x509.NewCertPool()
			if pool.AppendCertsFromPEM(pem) {
				transport.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
			}
		}
	}
	return &Client{
		baseURL: strings.TrimRight(opts.BaseURL, "/"),
		token:   opts.Token,
		http:    &http.Client{Timeout: opts.Timeout, Transport: transport},
	}, nil
}

// Do issues an authenticated request and decodes a JSON response into out.
func (c *Client) Do(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("cli: encode request: %w", err)
		}
		reader = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return fmt.Errorf("cli: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("cli: request %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("cli: read response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return apiError(resp.StatusCode, raw)
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("cli: decode response: %w", err)
	}
	return nil
}

// OfflineError carries the command the server persisted when the target screen
// had no live session. It is a distinct type so `screen navigate --wait` can
// print the record instead of only an error string (design §6.5).
type OfflineError struct {
	Message string
	Command commandItem
}

func (e *OfflineError) Error() string { return e.Message }

// apiError converts a JSON error body into a readable message.
func apiError(status int, raw []byte) error {
	var body struct {
		Error struct {
			Code    domain.ErrorCode `json:"code"`
			Message string           `json:"message"`
		} `json:"error"`
		Command *commandItem `json:"command"`
	}
	if err := json.Unmarshal(raw, &body); err == nil && body.Error.Code != "" {
		msg := fmt.Sprintf("server returned %d %s: %s", status, body.Error.Code, body.Error.Message)
		if body.Error.Code == domain.CodeScreenOffline && body.Command != nil {
			return &OfflineError{Message: msg, Command: *body.Command}
		}
		return errors.New(msg)
	}
	return fmt.Errorf("server returned %d: %s", status, strings.TrimSpace(string(raw)))
}

// ResolveToken applies the resolution order of design §6.8: --token, then
// ATRIUM_ADMIN_TOKEN, then the token file in the data directory.
func ResolveToken(explicit, dataDir string) string {
	if explicit != "" {
		return explicit
	}
	if v := os.Getenv(auth.EnvAdminTokenName); v != "" {
		return v
	}
	if dataDir != "" {
		if tok, err := auth.ReadAdminTokenFile(dataDir); err == nil {
			return tok
		}
	}
	return ""
}

// CAFileFor returns the local CA path inside a data directory.
func CAFileFor(dataDir string) string {
	if dataDir == "" {
		return ""
	}
	return filepath.Join(dataDir, "tls", "ca.crt")
}
