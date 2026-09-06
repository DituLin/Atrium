package tlsgen

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// Ensure creates the CA and the server certificate when they are missing and
// returns the paths the server should use.
func Ensure(p Params) (*Result, error) {
	return generate(p, false)
}

// Renew regenerates the server certificate, keeping the existing CA.
func Renew(p Params) (*Result, error) {
	return generate(p, true)
}

func generate(p Params, forceServer bool) (*Result, error) {
	if p.Now.IsZero() {
		p.Now = time.Now()
	}
	if err := os.MkdirAll(p.Dir, 0o700); err != nil {
		return nil, fmt.Errorf("tlsgen: create %s: %w", p.Dir, err)
	}
	caCertPath := filepath.Join(p.Dir, CACertFile)
	caKeyPath := filepath.Join(p.Dir, CAKeyFile)
	serverCertPath := filepath.Join(p.Dir, ServerCertFile)
	serverKeyPath := filepath.Join(p.Dir, ServerKeyFile)

	res := &Result{CACertPath: caCertPath, ServerCertPath: serverCertPath, ServerKeyPath: serverKeyPath}

	caCert, caKey, created, err := ensureCA(caCertPath, caKeyPath, p.Now)
	if err != nil {
		return nil, err
	}
	res.Created = created

	if !forceServer && !created && fileExists(serverCertPath) && fileExists(serverKeyPath) {
		if ok, err := serverCertCovers(serverCertPath, p); err == nil && ok {
			dns, ips := SANs(p.PublicURL, p.ExtraSANs)
			res.DNSNames = dns
			res.IPAddresses = ipStrings(ips)
			return res, nil
		}
	}

	dns, ips := SANs(p.PublicURL, p.ExtraSANs)
	serial, err := randomSerial()
	if err != nil {
		return nil, err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("tlsgen: generate server key: %w", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "Atrium server", Organization: []string{"Atrium"}},
		NotBefore:             p.Now.Add(-time.Hour),
		NotAfter:              p.Now.Add(ServerLifetime),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              dns,
		IPAddresses:           ips,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, caCert, &key.PublicKey, caKey)
	if err != nil {
		return nil, fmt.Errorf("tlsgen: sign server certificate: %w", err)
	}
	if err := writeCert(serverCertPath, der); err != nil {
		return nil, err
	}
	if err := writeKey(serverKeyPath, key); err != nil {
		return nil, err
	}
	res.DNSNames = dns
	res.IPAddresses = ipStrings(ips)
	res.Created = true
	return res, nil
}

func ensureCA(certPath, keyPath string, now time.Time) (*x509.Certificate, *ecdsa.PrivateKey, bool, error) {
	if fileExists(certPath) && fileExists(keyPath) {
		cert, key, err := loadCA(certPath, keyPath)
		if err != nil {
			return nil, nil, false, err
		}
		return cert, key, false, nil
	}
	serial, err := randomSerial()
	if err != nil {
		return nil, nil, false, err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, false, fmt.Errorf("tlsgen: generate CA key: %w", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "Atrium local CA", Organization: []string{"Atrium"}},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(CALifetime),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLenZero:        true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, false, fmt.Errorf("tlsgen: create CA certificate: %w", err)
	}
	if err := writeCert(certPath, der); err != nil {
		return nil, nil, false, err
	}
	if err := writeKey(keyPath, key); err != nil {
		return nil, nil, false, err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, false, fmt.Errorf("tlsgen: parse CA certificate: %w", err)
	}
	return cert, key, true, nil
}

func loadCA(certPath, keyPath string) (*x509.Certificate, *ecdsa.PrivateKey, error) {
	certPEM, err := os.ReadFile(certPath) //nolint:gosec // path is inside the data dir
	if err != nil {
		return nil, nil, fmt.Errorf("tlsgen: read CA certificate: %w", err)
	}
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return nil, nil, fmt.Errorf("tlsgen: CA certificate %s is not PEM", filepath.Base(certPath))
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("tlsgen: parse CA certificate: %w", err)
	}
	keyPEM, err := os.ReadFile(keyPath) //nolint:gosec // path is inside the data dir
	if err != nil {
		return nil, nil, fmt.Errorf("tlsgen: read CA key: %w", err)
	}
	kb, _ := pem.Decode(keyPEM)
	if kb == nil {
		return nil, nil, fmt.Errorf("tlsgen: CA key is not PEM")
	}
	key, err := x509.ParseECPrivateKey(kb.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("tlsgen: parse CA key: %w", err)
	}
	return cert, key, nil
}

// serverCertCovers reports whether the stored server certificate is still valid
// and carries every required SAN.
func serverCertCovers(path string, p Params) (bool, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // path is inside the data dir
	if err != nil {
		return false, err
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		return false, fmt.Errorf("tlsgen: server certificate is not PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return false, err
	}
	if p.Now.After(cert.NotAfter) {
		return false, nil
	}
	dns, ips := SANs(p.PublicURL, p.ExtraSANs)
	have := map[string]struct{}{}
	for _, n := range cert.DNSNames {
		have[n] = struct{}{}
	}
	for _, ip := range cert.IPAddresses {
		have[ip.String()] = struct{}{}
	}
	for _, n := range dns {
		if _, ok := have[n]; !ok {
			return false, nil
		}
	}
	for _, ip := range ips {
		if _, ok := have[ip.String()]; !ok {
			return false, nil
		}
	}
	return true, nil
}

// ExportCA returns the PEM bytes of the local CA certificate.
func ExportCA(dir string) ([]byte, error) {
	path := filepath.Join(dir, CACertFile)
	raw, err := os.ReadFile(path) //nolint:gosec // path is inside the data dir
	if err != nil {
		return nil, fmt.Errorf("tlsgen: read CA certificate: %w", err)
	}
	return raw, nil
}

func randomSerial() (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return nil, fmt.Errorf("tlsgen: serial number: %w", err)
	}
	return serial, nil
}

func writeCert(path string, der []byte) error {
	buf := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := os.WriteFile(path, buf, 0o644); err != nil { //nolint:gosec // certificates are public
		return fmt.Errorf("tlsgen: write %s: %w", filepath.Base(path), err)
	}
	return nil
}

func writeKey(path string, key *ecdsa.PrivateKey) error {
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return fmt.Errorf("tlsgen: marshal key: %w", err)
	}
	buf := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})
	if err := os.WriteFile(path, buf, 0o600); err != nil {
		return fmt.Errorf("tlsgen: write %s: %w", filepath.Base(path), err)
	}
	return nil
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

func ipStrings(ips []net.IP) []string {
	out := make([]string, 0, len(ips))
	for _, ip := range ips {
		out = append(out, ip.String())
	}
	return out
}
