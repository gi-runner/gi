package tlsconfig

import (
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestBuildPreservesExistingPolicyAndTrust(t *testing.T) {
	server := httptest.NewTLSServer(http.NotFoundHandler())
	defer server.Close()
	original := server.Certificate()
	changed := *original
	changed.SerialNumber = new(big.Int).Add(original.SerialNumber, big.NewInt(1))
	raw, err := x509.CreateCertificate(rand.Reader, &changed, &changed, original.PublicKey, server.TLS.Certificates[0].PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	extra := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: raw})
	path := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(path, extra, 0600); err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(original)
	saved := roots.Clone()
	base := &tls.Config{MinVersion: tls.VersionTLS13, ServerName: "original", RootCAs: roots}
	got, err := Build(base, Settings{CACert: path})
	if err != nil {
		t.Fatal(err)
	}
	expected := saved.Clone()
	if !expected.AppendCertsFromPEM(extra) {
		t.Fatal("invalid fixture")
	}
	if got == base || got.RootCAs == roots || got.MinVersion != tls.VersionTLS13 || got.ServerName != "original" || !got.RootCAs.Equal(expected) || !roots.Equal(saved) {
		t.Fatal("building a transport replaced or mutated the caller's TLS policy/trust")
	}
}

func TestParseAndInvalidCertificates(t *testing.T) {
	for _, input := range []any{nil, false} {
		got, err := Parse(input)
		if got != nil || err != nil {
			t.Fatalf("disabled TLS: %v %v", got, err)
		}
	}
	got, err := Parse(true)
	if err != nil || got.MinVersion != tls.VersionTLS12 {
		t.Fatalf("enabled TLS: %v %v", got, err)
	}
	for _, input := range []any{"yes", map[string]any{"cert": "cert"}, map[string]any{"key": "key"}, map[string]any{"caCert": "/missing/ca.pem"}} {
		if _, err := Parse(input); err == nil {
			t.Fatalf("accepted invalid TLS: %#v", input)
		}
	}
	path := filepath.Join(t.TempDir(), "invalid.pem")
	if err := os.WriteFile(path, []byte("not a certificate"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Build(nil, Settings{CACert: path}); err == nil {
		t.Fatal("accepted malformed CA")
	}
}
