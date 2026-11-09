package engine_test

import (
	"context"
	"encoding/json"
	"net"
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

func TestConfigureHTTPKeys(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hj, ok := w.(http.Hijacker); ok && r.URL.Path == "/flaky" && hits.Add(1) == 1 {
			if conn, _, err := hj.Hijack(); err == nil {
				_ = conn.Close()
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		session := ""
		if c, err := r.Cookie("session"); err == nil {
			session = c.Value
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"ctype": r.Header.Get("Content-Type"), "session": session})
	}))
	defer server.Close()
	runRelease(t, "configure.feature", `Feature: configure
Scenario: http keys
* url base
* configure cookies = { session: 'abc' }
* configure charset = 'utf-8'
* configure localAddress = '127.0.0.1'
* request { a: 1 }
* method post
* match response == { ctype: 'application/json; charset=utf-8', session: 'abc' }
* configure cookies = null
* configure charset = null
* request { a: 1 }
* method post
* match response == { ctype: 'application/json', session: '' }
* configure httpRetryEnabled = true
* configure retry = { count: 2, interval: 0 }
* path 'flaky'
* method get
* status 200
* configure logPrettyRequest = true
* configure lowerCaseResponseHeaders = true
`, engine.Options{Vars: map[string]any{"base": server.URL}, HTTP: httpx.New(nil)})
}

func TestConfigureReportAndLogging(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Token", "t0ken")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"card":"4111-1111","ok":true}`))
	}))
	defer server.Close()
	f, err := feature.Parse("logging.feature", `Feature: logging
Scenario: shaped
* configure logging = { pretty: true, mask: { headers: ['X-Token'], patterns: ['\\d{4}-\\d{4}'] } }
* configure report = { showLog: false, showAllSteps: false }
* print 'hidden'
* url base
* method get
`)
	if err != nil {
		t.Fatal(err)
	}
	res, err := engine.RunFile(t.Context(), f, engine.Options{Vars: map[string]any{"base": server.URL}, HTTP: httpx.New(nil)})
	if err != nil || !res.Scenarios[0].Passed {
		t.Fatalf("%+v %v", res, err)
	}
	steps := res.Scenarios[0].Steps
	if len(steps) != 1 || steps[0].HTTP == nil {
		t.Fatalf("terse report kept %+v", steps)
	}
	ex := steps[0].HTTP
	if ex.ResponseHeaders["X-Token"] != "***" || strings.Contains(ex.ResponseBody, "4111") || !strings.Contains(ex.ResponseBody, "\n  \"ok\"") {
		t.Fatalf("headers %v body %q", ex.ResponseHeaders, ex.ResponseBody)
	}
}

func TestAbortedAndRemainingSteps(t *testing.T) {
	f, err := feature.Parse("abort.feature", `Feature: abort
Scenario: aborted
* configure abortedStepsShouldPass = true
* karate.abort()
* match 1 == 2

Scenario: failed
* match 1 == 2
* match 1 == 1
`)
	if err != nil {
		t.Fatal(err)
	}
	res, err := engine.RunFile(t.Context(), f, engine.Options{})
	if err != nil {
		t.Fatal(err)
	}
	aborted, failed := res.Scenarios[0], res.Scenarios[1]
	if !aborted.Passed || len(aborted.Steps) != 3 || !aborted.Steps[2].Passed || aborted.Steps[2].Skipped {
		t.Fatalf("aborted %+v", aborted.Steps)
	}
	if failed.Passed || len(failed.Steps) != 2 || !failed.Steps[1].Skipped {
		t.Fatalf("failed %+v", failed.Steps)
	}
}

func TestAbortSuiteOnFailure(t *testing.T) {
	f, err := feature.Parse("suite.feature", `Feature: suite
Background:
* configure abortSuiteOnFailure = true

Scenario: breaks
* match 1 == 2

Scenario: never runs
* match 1 == 1
`)
	if err != nil {
		t.Fatal(err)
	}
	res, err := engine.RunFile(t.Context(), f, engine.Options{Jobs: 1, Suite: suite.Open(t.TempDir(), "")})
	if err != nil {
		t.Fatal(err)
	}
	if res.Scenarios[0].Passed || !res.Scenarios[1].Skipped {
		t.Fatalf("%+v", res.Scenarios)
	}
}

func TestCallSingleCache(t *testing.T) {
	dir := t.TempDir()
	cache := filepath.Join(dir, "cache")
	if err := os.WriteFile(filepath.Join(dir, "token.js"), []byte("function() { return { token: 'x' + Math.random() } }"), 0o600); err != nil {
		t.Fatal(err)
	}
	body := "Feature: cache\nScenario: cached\n* configure callSingleCache = { minutes: 5, dir: '" + filepath.ToSlash(cache) + "' }\n* def first = karate.callSingle('token.js')\n"
	path := filepath.Join(dir, "cache.feature")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	tokens := make([]any, 2)
	for i := range tokens {
		f, err := feature.ParseFile(path)
		if err != nil {
			t.Fatal(err)
		}
		res, err := engine.RunFile(t.Context(), f, engine.Options{Suite: suite.Open(path, "")})
		if err != nil || !res.Scenarios[0].Passed {
			t.Fatalf("%+v %v", res, err)
		}
		first, _ := res.Scenarios[0].Vars["first"].(map[string]any)
		tokens[i] = first["token"]
	}
	if tokens[0] == nil || tokens[0] != tokens[1] {
		t.Fatalf("callSingleCache did not persist: %v", tokens)
	}
	entries, _ := os.ReadDir(cache)
	if len(entries) != 1 {
		t.Fatalf("cache dir has %d entries", len(entries))
	}
}

func TestConfigureRejectsBadValues(t *testing.T) {
	for _, line := range []string{
		"configure driver = { type: 'geckodriver' }",
		"configure driver = { colour: 'red' }",
		"configure localAddress = 'not-an-ip'",
		"configure logging = { mask: { jsonPaths: ['$.a'] } }",
		"configure report = { showLog: 'yes' }",
		"configure cors = 'yes'",
		"configure nope = 1",
	} {
		f, err := feature.Parse("bad.feature", "Feature: bad\nScenario: bad\n* "+line+"\n")
		if err != nil {
			t.Fatal(err)
		}
		res, err := engine.RunFile(t.Context(), f, engine.Options{})
		if err != nil || res.Scenarios[0].Passed {
			t.Errorf("%s: accepted (%v)", line, err)
		}
	}
	f, _ := feature.Parse("driver.feature", "Feature: d\nScenario: d\n* configure driver = { type: 'chrome', headless: false, executable: '/bin/chrome', addOptions: ['--window-size=800,600'], showDriverLog: true }\n* driver 'about:blank'\n")
	var got engine.DriverConfig
	_, err := engine.RunFile(t.Context(), f, engine.Options{NewPage: func(_ context.Context, cfg engine.DriverConfig) (engine.Page, error) {
		got = cfg
		return nil, net.ErrClosed
	}})
	if err != nil || got.Headless || got.Executable != "/bin/chrome" || len(got.Args) != 1 {
		t.Fatalf("%+v %v", got, err)
	}
}
