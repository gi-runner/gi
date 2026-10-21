package kafkax

import (
	"context"
	"crypto/tls"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kfake"
	"github.com/twmb/franz-go/pkg/kgo"
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

func TestOffsetAndConsumerGroup(t *testing.T) {
	cluster, err := kfake.NewCluster(
		kfake.NumBrokers(1),
		kfake.SeedTopics(1, "off"),
		kfake.SeedTopics(1, "grp"),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer cluster.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	addr := cluster.ListenAddrs()[0]

	prod, err := Dial(ctx, map[string]any{"brokers": addr, "topic": "off"})
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{"a", "b", "c"} {
		if err := prod.Send(ctx, body); err != nil {
			t.Fatal(err)
		}
	}
	if err := prod.Close(); err != nil {
		t.Fatal(err)
	}

	from, err := Dial(ctx, map[string]any{"brokers": addr, "topic": "off", "offset": 1})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = from.Close() }()
	got, err := from.Receive(ctx)
	if err != nil || got != "b" {
		t.Fatalf("offset 1: %v %v", got, err)
	}

	end, err := Dial(ctx, map[string]any{"brokers": addr, "topic": "off", "offset": "end"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = end.Close() }()
	time.Sleep(300 * time.Millisecond)
	if err := end.Send(ctx, "d"); err != nil {
		t.Fatal(err)
	}
	got, err = end.Receive(ctx)
	if err != nil || got != "d" {
		t.Fatalf("offset end: %v %v", got, err)
	}

	writer, err := Dial(ctx, map[string]any{"brokers": addr, "topic": "grp"})
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Send(ctx, "one"); err != nil {
		t.Fatal(err)
	}
	_ = writer.Close()
	first, err := Dial(ctx, map[string]any{"brokers": addr, "topic": "grp", "group": "readers"})
	if err != nil {
		t.Fatal(err)
	}
	got, err = first.Receive(ctx)
	if err != nil || got != "one" {
		t.Fatalf("group read: %v %v", got, err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := Dial(ctx, map[string]any{"brokers": addr, "topic": "grp", "group": "readers"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = second.Close() }()
	if err := second.Send(ctx, "two"); err != nil {
		t.Fatal(err)
	}
	got, err = second.Receive(ctx)
	if err != nil || got != "two" {
		t.Fatalf("group resume: %v %v", got, err)
	}
}

func TestCommitFailureKeepsTheRecord(t *testing.T) {
	var committed []string
	fails := 0
	l := &link{group: true, pending: []record{
		{value: "one", raw: &kgo.Record{Value: []byte("one")}},
		{value: "two", raw: &kgo.Record{Value: []byte("two")}},
	}}
	l.commit = func(_ context.Context, rec *kgo.Record) error {
		if fails == 0 {
			fails++
			return errors.New("commit failed")
		}
		committed = append(committed, string(rec.Value))
		return nil
	}
	if _, err := l.Receive(t.Context()); err == nil {
		t.Fatal("commit failure returned a record")
	}
	if len(l.pending) != 2 || l.pending[0].value != "one" {
		t.Fatalf("pending after failure: %+v", l.pending)
	}
	got, err := l.Receive(t.Context())
	if err != nil || got != "one" {
		t.Fatalf("retry: %v %v", got, err)
	}
	got, err = l.Receive(t.Context())
	if err != nil || got != "two" {
		t.Fatalf("next: %v %v", got, err)
	}
	if len(committed) != 2 || committed[0] != "one" || committed[1] != "two" {
		t.Fatalf("committed: %v", committed)
	}
}

func TestInvalidSASLAndTLS(t *testing.T) {
	for _, spec := range []map[string]any{
		{"brokers": "127.0.0.1:1", "topic": "events", "sasl": "ada"},
		{"brokers": "127.0.0.1:1", "topic": "events", "sasl": map[string]any{"username": "ada"}},
		{"brokers": "127.0.0.1:1", "topic": "events", "sasl": map[string]any{"mechanism": "GSSAPI", "username": "ada", "password": "x"}},
		{"brokers": "127.0.0.1:1", "topic": "events", "tls": "yes"},
		{"brokers": "127.0.0.1:1", "topic": "events", "tls": map[string]any{"cert": "only-cert"}},
		{"brokers": "127.0.0.1:1", "topic": "events", "offset": "middle"},
		{"brokers": "127.0.0.1:1", "topic": "events", "offset": -1},
		{"brokers": "127.0.0.1:1", "topic": "events", "group": ""},
		{"brokers": "127.0.0.1:1", "topic": "events", "group": 3},
	} {
		if _, err := Dial(t.Context(), spec); err == nil {
			t.Errorf("accepted invalid configuration: %#v", spec)
		}
	}
}
