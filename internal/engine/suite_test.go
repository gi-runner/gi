package engine_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/gi-runner/gi/internal/engine"
	"github.com/gi-runner/gi/internal/feature"
	"github.com/gi-runner/gi/internal/httpx"
	"github.com/gi-runner/gi/internal/suite"
)

func TestSuiteConfigCallAndRetry(t *testing.T) {
	var ticks, tries atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("GET /echo", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]string{"from": r.Header.Get("X-From"), "token": r.Header.Get("X-Token")})
	})
	mux.HandleFunc("POST /tick", func(w http.ResponseWriter, r *http.Request) {
		n := ticks.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"n":` + itoa(n) + `}`))
	})
	mux.HandleFunc("GET /retry", func(w http.ResponseWriter, r *http.Request) {
		if tries.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("gi-config.js", `
function fn() {
  var config = { baseUrl: '`+srv.URL+`', token: gi.callSingle('auth.feature').token }
  gi.configure('headers', read('headers.js'))
  return config
}
`)
	write("gi-config-staging.js", "function fn() { return { tier: 'staging' } }\n")
	write("headers.js", "function fn() { return { 'X-From': 'headers.js', 'X-Token': gi.get('token') } }\n")
	write("auth.feature", "Feature:\nScenario:\n* def token = 'abc'\n")
	write("double.feature", "Feature:\nScenario:\n* def doubled = __arg.n * 2\n")
	write("tick.feature", "Feature:\nScenario:\n* url baseUrl\n* path 'tick'\n* method post\n* def n = response.n\n")
	write("api.feature", `
Feature:
@env=dev
Scenario: not staging
* match 1 == 2

@env=staging
Scenario: headers from config
* url baseUrl
* path 'echo'
* method get
* match response.from == 'headers.js'
* match response.token == 'abc'
* match tier == 'staging'

Scenario: call with args
* def out = call read('double.feature') { n: 21 }
* match out.doubled == 42

Scenario: callonce first
* callonce read('tick.feature')
* match n == 1

Scenario: callonce second
* callonce read('tick.feature')
* match n == 1

Scenario: retry until ready
* url baseUrl
* path 'retry'
* configure retry = { count: 5, interval: 1 }
* retry until responseStatus == 200
* method get
* status 200
`)

	f, err := feature.ParseFile(filepath.Join(dir, "api.feature"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := engine.RunFile(context.Background(), f, engine.Options{
		HTTP:  httpx.New(srv.Client()),
		Env:   "staging",
		Suite: suite.Open(filepath.Join(dir, "api.feature"), "staging"),
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, sc := range res.Scenarios {
		if sc.Name == "not staging" {
			if !sc.Skipped {
				t.Fatal("@env=dev ran under staging")
			}
			continue
		}
		if !sc.Passed {
			t.Errorf("%s\n%s", sc.Name, sc.Error)
		}
	}
	if ticks.Load() != 1 {
		t.Fatalf("callonce hit the server %d times", ticks.Load())
	}
}

func itoa(n int32) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
