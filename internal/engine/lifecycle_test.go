package engine_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gi-runner/gi/internal/engine"
	"github.com/gi-runner/gi/internal/feature"
	"github.com/gi-runner/gi/internal/httpx"
	"github.com/gi-runner/gi/internal/suite"
)

func TestHooksAndSetup(t *testing.T) {
	var hits atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("GET /token", func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"abc"}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	config := "function fn() {\n  gi.configure('beforeScenario', function(){ gi.set('ready', true) })\n  return {}\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "gi-config.js"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	src := `
Feature:

Background:
* def fromBack = true

@setup
Scenario:
* url baseUrl
* path 'token'
* method get
* def token = response.token

Scenario: first
* match ready == true
* def auth = gi.setupOnce()
* match auth.token == 'abc'
* match auth.fromBack == '#notpresent'
* configure afterScenario = function(){ gi.set('closed', true) }
* assert 1 == 2

Scenario: second
* def auth = gi.setupOnce()
* match auth.token == 'abc'
* match auth.fromBack == '#notpresent'
`
	path := filepath.Join(dir, "life.feature")
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := feature.ParseFile(path)
	if err != nil {
		t.Fatal(err)
	}
	res, err := engine.RunFile(context.Background(), f, engine.Options{
		HTTP:  httpx.New(srv.Client()),
		Vars:  map[string]any{"baseUrl": srv.URL},
		Suite: suite.Open(path, ""),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Scenarios) != 2 {
		t.Fatalf("scenarios = %d, want 2", len(res.Scenarios))
	}
	if hits.Load() != 1 {
		t.Fatalf("token requests = %d, want 1", hits.Load())
	}
	first := res.Scenarios[0]
	if first.Passed || !strings.Contains(first.Error, "assert failed") {
		t.Fatalf("first: passed=%v err=%s", first.Passed, first.Error)
	}
	if first.Vars["closed"] != true {
		t.Fatalf("afterScenario did not run: %#v", first.Vars["closed"])
	}
	if !hookPassed(first, "beforeScenario") || !hookPassed(first, "afterScenario") {
		t.Fatalf("hooks: %+v", first.Steps)
	}
	second := res.Scenarios[1]
	if !second.Passed {
		t.Fatal(second.Error)
	}
	if !hookPassed(second, "beforeScenario") || hookPassed(second, "afterScenario") {
		t.Fatalf("second hooks: %+v", second.Steps)
	}
}

func hookPassed(sc engine.ScenarioResult, name string) bool {
	for _, st := range sc.Steps {
		if st.Text == name {
			return st.Passed
		}
	}
	return false
}
