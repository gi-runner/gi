package wsx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestTextAndBinaryWithHandshakeOptions(t *testing.T) {
	seen := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer local-token" {
			http.Error(w, "missing auth", http.StatusUnauthorized)
			return
		}
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{"gi-test"}})
		if err != nil {
			return
		}
		defer func() { _ = c.CloseNow() }()
		seen <- c.Subprotocol()
		for {
			kind, data, err := c.Read(r.Context())
			if err != nil {
				return
			}
			if err = c.Write(r.Context(), kind, data); err != nil {
				return
			}
		}
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	link, err := Dial(ctx, map[string]any{"url": "ws" + strings.TrimPrefix(srv.URL, "http"), "headers": map[string]any{"Authorization": "Bearer local-token"}, "subprotocols": []any{"gi-test"}, "maxMessageSize": int64(128)})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = link.Close() }()
	if protocol := <-seen; protocol != "gi-test" {
		t.Fatalf("subprotocol %q", protocol)
	}
	if err := link.Send(ctx, "hello"); err != nil {
		t.Fatal(err)
	}
	text, err := link.Receive(ctx)
	if err != nil || text != "hello" {
		t.Fatalf("text: %#v %v", text, err)
	}
	payload := []byte{0, 0xff, 1, 2}
	if err := link.Send(ctx, payload); err != nil {
		t.Fatal(err)
	}
	binary, err := link.Receive(ctx)
	if err != nil {
		t.Fatal(err)
	}
	raw, ok := binary.([]byte)
	if !ok || string(raw) != string(payload) {
		t.Fatalf("binary corrupted: %#v", binary)
	}
	if err := link.Send(ctx, strings.Repeat("x", 256)); err != nil {
		t.Fatal(err)
	}
	if _, err := link.Receive(ctx); err == nil {
		t.Fatal("message limit ignored")
	}
}

func TestInvalidOptions(t *testing.T) {
	for _, option := range []map[string]any{
		{"headers": "bad"}, {"subprotocols": "bad"}, {"subprotocols": []any{42}},
		{"maxMessageSize": int64(0)}, {"maxMessageSize": 1.5},
	} {
		option["url"] = "ws://127.0.0.1:1"
		if _, err := Dial(t.Context(), option); err == nil {
			t.Fatalf("accepted %#v", option)
		}
	}
}
