package httpx

import (
	"testing"

	"github.com/gi-runner/gi/internal/engine"
)

func TestTransportRejectsUnpairedClientKey(t *testing.T) {
	client := New(nil)
	if _, err := client.transport(engine.TransportSettings{Key: "client-key.pem"}); err == nil {
		t.Fatal("HTTP transport silently ignored an unpaired TLS client key")
	}
}
