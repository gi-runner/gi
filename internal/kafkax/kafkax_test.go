package kafkax

import (
	"context"
	"crypto/tls"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kfake"
)

func TestSASLAndTLSRoundTrip(t *testing.T) {
	certServer := httptest.NewTLSServer(http.NotFoundHandler())
	pair := certServer.TLS.Certificates[0]
	leaf := certServer.Certificate()
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leaf.Raw})
	serverName := ""
	if len(leaf.DNSNames) > 0 {
		serverName = leaf.DNSNames[0]
	}
	certServer.Close()
	certPath := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(certPath, ca, 0o600); err != nil {
		t.Fatal(err)
	}
	cluster, err := kfake.NewCluster(
		kfake.NumBrokers(1),
		kfake.SeedTopics(1, "plain-events"),
		kfake.SeedTopics(1, "scram-events"),
		kfake.EnableSASL(),
		kfake.Superuser("PLAIN", "ada", "secret"),
		kfake.Superuser("SCRAM-SHA-512", "ada", "secret"),
		kfake.TLS(&tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{pair}}),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer cluster.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	addr := cluster.ListenAddrs()[0]
	tlsSpec := map[string]any{"caCert": certPath, "serverName": serverName}

	plainLink, err := Dial(ctx, map[string]any{
		"brokers": addr,
		"topic":   "plain-events",
		"sasl":    map[string]any{"mechanism": "PLAIN", "username": "ada", "password": "secret"},
		"tls":     tlsSpec,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = plainLink.Close() }()
	if err := plainLink.Send(ctx, "one"); err != nil {
		t.Fatal(err)
	}
	got, err := plainLink.Receive(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got != "one" {
		t.Fatalf("plain: %#v", got)
	}

	scram, err := Dial(ctx, map[string]any{
		"brokers": addr,
		"topic":   "scram-events",
		"sasl":    map[string]any{"mechanism": "SCRAM-SHA-512", "username": "ada", "password": "secret"},
		"tls":     tlsSpec,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = scram.Close() }()
	if err := scram.Send(ctx, map[string]any{"n": 2}); err != nil {
		t.Fatal(err)
	}
	got, err = scram.Receive(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got != `{"n":2}` {
		t.Fatalf("scram: %#v", got)
	}

	denied, err := Dial(ctx, map[string]any{
		"brokers": addr,
		"topic":   "plain-events",
		"sasl":    map[string]any{"username": "ada", "password": "wrong"},
		"tls":     tlsSpec,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = denied.Close() }()
	if err := denied.Send(ctx, "nope"); err == nil {
		t.Fatal("wrong password produced a record")
	}
}

func TestInvalidSASLAndTLS(t *testing.T) {
	for _, spec := range []map[string]any{
		{"brokers": "127.0.0.1:1", "topic": "events", "sasl": "ada"},
		{"brokers": "127.0.0.1:1", "topic": "events", "sasl": map[string]any{"username": "ada"}},
		{"brokers": "127.0.0.1:1", "topic": "events", "sasl": map[string]any{"mechanism": "GSSAPI", "username": "ada", "password": "x"}},
		{"brokers": "127.0.0.1:1", "topic": "events", "tls": "yes"},
		{"brokers": "127.0.0.1:1", "topic": "events", "tls": map[string]any{"cert": "only-cert"}},
	} {
		if _, err := Dial(t.Context(), spec); err == nil {
			t.Errorf("accepted invalid configuration: %#v", spec)
		}
	}
}
