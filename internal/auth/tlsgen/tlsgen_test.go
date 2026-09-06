package tlsgen_test

import (
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/DituLin/Atritum/internal/auth/tlsgen"
)

func parseCert(t *testing.T, path string) *x509.Certificate {
	t.Helper()
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	block, _ := pem.Decode(raw)
	require.NotNil(t, block)
	cert, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)
	return cert
}

func TestEnsureCreatesCAAndServerCertWithSANs(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "tls")
	now := time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)

	res, err := tlsgen.Ensure(tlsgen.Params{
		Dir:       dir,
		PublicURL: "https://192.168.1.10:8443",
		ExtraSANs: []string{"macmini.local"},
		Now:       now,
	})
	require.NoError(t, err)
	require.True(t, res.Created)

	for _, name := range []string{tlsgen.CACertFile, tlsgen.CAKeyFile, tlsgen.ServerCertFile, tlsgen.ServerKeyFile} {
		st, err := os.Stat(filepath.Join(dir, name))
		require.NoError(t, err, name)
		require.False(t, st.IsDir())
	}

	keySt, err := os.Stat(filepath.Join(dir, tlsgen.ServerKeyFile))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), keySt.Mode().Perm(), "private keys must not be world readable")

	server := parseCert(t, res.ServerCertPath)
	require.ElementsMatch(t, []string{"localhost", "macmini.local"}, server.DNSNames)
	var ips []string
	for _, ip := range server.IPAddresses {
		ips = append(ips, ip.String())
	}
	require.Contains(t, ips, "127.0.0.1")
	require.Contains(t, ips, "192.168.1.10")
	require.Contains(t, ips, "::1")
	require.Equal(t, now.Add(tlsgen.ServerLifetime), server.NotAfter.UTC())

	ca := parseCert(t, res.CACertPath)
	require.True(t, ca.IsCA)
	require.Equal(t, now.Add(tlsgen.CALifetime), ca.NotAfter.UTC())

	pool := x509.NewCertPool()
	pool.AddCert(ca)
	_, err = server.Verify(x509.VerifyOptions{Roots: pool, CurrentTime: now, DNSName: "localhost"})
	require.NoError(t, err, "the server certificate must chain to the generated CA")
}

func TestEnsureIsIdempotentAndRenewKeepsCA(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "tls")
	params := tlsgen.Params{Dir: dir, PublicURL: "https://192.168.1.10:8443"}

	first, err := tlsgen.Ensure(params)
	require.NoError(t, err)
	firstServer := parseCert(t, first.ServerCertPath)
	firstCA := parseCert(t, first.CACertPath)

	second, err := tlsgen.Ensure(params)
	require.NoError(t, err)
	require.False(t, second.Created, "a second Ensure reuses the existing material")
	require.Equal(t, firstServer.SerialNumber, parseCert(t, second.ServerCertPath).SerialNumber)

	renewed, err := tlsgen.Renew(params)
	require.NoError(t, err)
	require.NotEqual(t, firstServer.SerialNumber, parseCert(t, renewed.ServerCertPath).SerialNumber)
	require.Equal(t, firstCA.SerialNumber, parseCert(t, renewed.CACertPath).SerialNumber, "renew keeps the CA")
}

func TestEnsureRegeneratesWhenSANsChange(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "tls")
	first, err := tlsgen.Ensure(tlsgen.Params{Dir: dir, PublicURL: "https://192.168.1.10:8443"})
	require.NoError(t, err)
	before := parseCert(t, first.ServerCertPath).SerialNumber

	second, err := tlsgen.Ensure(tlsgen.Params{
		Dir: dir, PublicURL: "https://192.168.1.10:8443", ExtraSANs: []string{"macmini.local"},
	})
	require.NoError(t, err)
	require.True(t, second.Created)
	require.NotEqual(t, before, parseCert(t, second.ServerCertPath).SerialNumber)
	require.Contains(t, parseCert(t, second.ServerCertPath).DNSNames, "macmini.local")
}

func TestSANsAlwaysIncludeLoopback(t *testing.T) {
	dns, ips := tlsgen.SANs("", nil)
	require.Equal(t, []string{"localhost"}, dns)
	var out []string
	for _, ip := range ips {
		out = append(out, ip.String())
	}
	require.ElementsMatch(t, []string{"127.0.0.1", "::1"}, out)
}

func TestExportCA(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "tls")
	_, err := tlsgen.Ensure(tlsgen.Params{Dir: dir, PublicURL: "https://127.0.0.1:8443"})
	require.NoError(t, err)
	pemBytes, err := tlsgen.ExportCA(dir)
	require.NoError(t, err)
	require.Contains(t, string(pemBytes), "BEGIN CERTIFICATE")
}
