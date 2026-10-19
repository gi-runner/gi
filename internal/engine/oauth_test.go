package engine_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gi-runner/gi/internal/engine"
	"github.com/gi-runner/gi/internal/feature"
	"github.com/gi-runner/gi/internal/httpx"
)

func TestOAuthClientCredentials(t *testing.T) {
	var hits atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		b, _ := io.ReadAll(r.Body)
		body := string(b)
		for _, want := range []string{"grant_type=client_credentials", "client_id=id", "client_secret=secret", "scope=read"} {
			if !strings.Contains(body, want) {
				http.Error(w, body, http.StatusBadRequest)
				return
			}
		}
		if r.Header.Get("Accept") != "application/json" {
			http.Error(w, "accept", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"tok","token_type":"bearer"}`))
	})
	mux.HandleFunc("GET /secure", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]string{"authorization": r.Header.Get("Authorization")})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	src := `
Feature:

Scenario: token is fetched once
* configure auth = { type: 'oauth2', accessTokenUrl: '/token', clientId: 'id', clientSecret: 'secret', scope: 'read' }
* url baseUrl
* path 'secure'
* method get
* match response.authorization == 'Bearer tok'
* path 'secure'
* method get
* match response.authorization == 'Bearer tok'
`
	f, err := feature.Parse("oauth.feature", src)
	if err != nil {
		t.Fatal(err)
	}
	res, err := engine.RunFile(context.Background(), f, engine.Options{
		HTTP: httpx.New(srv.Client()),
		Vars: map[string]any{"baseUrl": srv.URL},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Scenarios) != 1 || !res.Scenarios[0].Passed {
		t.Fatal(res.Scenarios[0].Error)
	}
	if hits.Load() != 1 {
		t.Fatalf("token requests = %d, want 1", hits.Load())
	}
}
