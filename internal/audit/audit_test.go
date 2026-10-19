package audit

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gi-runner/gi/internal/engine"
	"github.com/gi-runner/gi/internal/feature"
	"github.com/gi-runner/gi/internal/httpx"
	"github.com/gi-runner/gi/internal/load"
	"github.com/gi-runner/gi/internal/match"
	"github.com/gi-runner/gi/internal/mock"
	"github.com/gi-runner/gi/internal/script"
	"github.com/gi-runner/gi/internal/value"
)

func featureOf(t *testing.T, path, src string) *feature.File {
	t.Helper()
	f, e := feature.Parse(path, src)
	if e != nil {
		t.Fatal(e)
	}
	return f
}
func TestEachNotEqual(t *testing.T) {
	r := match.MatchEach([]any{1}, 1, match.NotEquals, match.Context{})
	if r.Pass {
		t.Fatal("false positive: match each [1] != 1 passed")
	}
}
func TestEachNotContains(t *testing.T) {
	r := match.MatchEach([]any{[]any{1}}, []any{1}, match.NotContains, match.Context{})
	if r.Pass {
		t.Fatal("false positive: match each [[1]] !contains [1] passed")
	}
}
func TestLargeContainsOnly(t *testing.T) {
	a := []any{1, 2}
	e := []any{"#number", 1}
	for n := 3; n <= 13; n++ {
		a = append(a, n)
		e = append(e, n)
	}
	r := match.Match(a, e, match.ContainsOnly, match.Context{})
	if !r.Pass {
		t.Fatal("valid unordered matching rejected at 13 items:", r.Message)
	}
}
func TestScriptMutation(t *testing.T) {
	v := script.New()
	v.Set("n", int64(1))
	if _, e := v.Eval("n = 2"); e != nil {
		t.Fatal(e)
	}
	n, e := v.Eval("n")
	if e != nil || n != int64(2) {
		t.Fatalf("JS assigned n=2 but bare lookup returned %v (%v)", n, e)
	}
}
func TestScriptDoubleExecution(t *testing.T) {
	v := script.New()
	v.Set("n", int64(0))
	_, _ = v.Eval("(function(){ n++; throw new Error('boom'); })()")
	n, e := v.Eval("n + 0")
	if e != nil || n != int64(1) {
		t.Fatalf("one throwing expression ran %v times (%v)", n, e)
	}
}
func TestHTTPDuration(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		time.Sleep(120 * time.Millisecond)
		_, _ = io.WriteString(w, "done")
	}))
	defer s.Close()
	r, e := httpx.New(s.Client()).Send(context.Background(), engine.Call{Method: "GET", URL: s.URL, Follow: true})
	if e != nil {
		t.Fatal(e)
	}
	if r.Duration < 100*time.Millisecond {
		t.Fatalf("120ms body latency recorded as %v", r.Duration)
	}
}
func TestHTTPTruncation(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, strings.Repeat("x", (8<<20)+1))
	}))
	defer s.Close()
	r, e := httpx.New(s.Client()).Send(context.Background(), engine.Call{Method: "GET", URL: s.URL, Follow: true})
	if e == nil && len(r.Body) == 8<<20 {
		t.Fatal("oversized response silently truncated with nil error")
	}
}
func TestEscapedPath(t *testing.T) {
	var uri string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { uri = r.RequestURI }))
	defer s.Close()
	f := featureOf(t, "path.feature", fmt.Sprintf("Feature: path\nScenario: path\n* url '%s'\n* path 'a b'\n* method get\n", s.URL))
	r, e := engine.RunFile(context.Background(), f, engine.Options{HTTP: httpx.New(s.Client())})
	if e != nil || !r.Passed() {
		t.Fatalf("%v %+v", e, r)
	}
	if uri != "/a%20b" {
		t.Fatalf("expected /a%%20b got %s", uri)
	}
}
func TestCalledRequestObservers(t *testing.T) {
	dir := t.TempDir()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer s.Close()
	if err := os.WriteFile(filepath.Join(dir, "child.feature"), []byte(fmt.Sprintf("Feature: child\nScenario: child\n* url '%s'\n* method get\n", s.URL)), 0o600); err != nil {
		t.Fatal(err)
	}
	f := featureOf(t, filepath.Join(dir, "parent.feature"), "Feature: parent\nScenario: parent\n* call 'child.feature'\n")
	seen, before := 0, 0
	r, e := engine.RunFile(context.Background(), f, engine.Options{HTTP: httpx.New(s.Client()), OnRequest: func(engine.Exchange) { seen++ }, BeforeRequest: func(context.Context) error { before++; return nil }})
	if e != nil || !r.Passed() {
		t.Fatalf("%v %+v", e, r)
	}
	if seen != 1 || before != 1 {
		t.Fatalf("child HTTP callback counts: OnRequest=%d BeforeRequest=%d", seen, before)
	}
}
func TestCookiePersistence(t *testing.T) {
	count := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		if count == 1 {
			http.SetCookie(w, &http.Cookie{
				Name: "sid", Value: "abc", Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode,
			})
			return
		}
		if _, e := r.Cookie("sid"); e != nil {
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	defer s.Close()
	f := featureOf(t, "cookies.feature", fmt.Sprintf("Feature: cookies\nScenario: cookies\n* url '%s'\n* method get\n* method get\n* status 200\n", s.URL))
	r, e := engine.RunFile(context.Background(), f, engine.Options{HTTP: httpx.New(nil)})
	if e != nil || !r.Passed() {
		t.Fatalf("cookie session not retained: %v %+v", e, r.Scenarios)
	}
}
func TestMockRouteExpressions(t *testing.T) {
	f := featureOf(t, "mock.feature", "Feature: mock\nScenario: pathMatches('/one')\n* def response = 'one'\nScenario: pathMatches('/two')\n* def response = 'two'\n")
	w := httptest.NewRecorder()
	mock.Handler(f).ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/two", nil))
	if w.Body.String() != "two" {
		t.Fatalf("GET /two returned %q", w.Body.String())
	}
}
func TestMockHeaders(t *testing.T) {
	f := featureOf(t, "mock.feature", "Feature: mock\nScenario: response\n* def responseHeaders = { 'X-Test': 'yes' }\n* def response = 'ok'\n")
	w := httptest.NewRecorder()
	mock.Handler(f).ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
	if w.Header().Get("X-Test") != "yes" {
		t.Fatal("responseHeaders was ignored")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type cancelBody struct {
	io.Reader
	cancel context.CancelFunc
}

func (b cancelBody) Close() error { b.cancel(); return nil }
func TestLoadCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: cancelBody{strings.NewReader("ok"), cancel}, Request: r}, nil
	})}
	f := featureOf(t, "load.feature", "Feature: load\nScenario: load\n* url 'http://test.invalid'\n* method get\n")
	r, e := load.Run(ctx, f, load.Config{VUs: 1, Duration: time.Hour, Client: client, Timeout: time.Second})
	if e == nil && r.Passed {
		t.Fatalf("cancelled 1h load run reported success after %v", r.Elapsed)
	}
}
func TestLargeJSONInteger(t *testing.T) {
	v, e := value.Parse([]byte(`9007199254740993`))
	if e != nil {
		t.Fatal(e)
	}
	if v != int64(9007199254740993) {
		t.Fatalf("JSON integer changed to %v", v)
	}
}

func TestScriptDeadline(t *testing.T) {
	f := featureOf(t, "deadline.feature", "Feature: deadline\nScenario: deadline\n* def n = (function(){ var end = Date.now()+100; while(Date.now()<end){}; return 1; })()\n")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	start := time.Now()
	r, e := engine.RunFile(ctx, f, engine.Options{})
	if e == nil && r.Passed() {
		t.Fatalf("expired 10ms scenario reported success after %v", time.Since(start))
	}
}
func TestCallArgumentBindings(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "child.feature"), []byte("Feature: child\nScenario: child\n* match name == 'Ada'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f := featureOf(t, filepath.Join(dir, "parent.feature"), "Feature: parent\nScenario: parent\n* call 'child.feature' { name: 'Ada' }\n")
	r, e := engine.RunFile(context.Background(), f, engine.Options{})
	if e != nil || !r.Passed() {
		t.Fatalf("object call argument not bound: %v %+v", e, r.Scenarios)
	}
}
func TestOutlineRowBindings(t *testing.T) {
	f := featureOf(t, "outline.feature", "Feature: outline\nScenario Outline: row\n* match name == 'Ada'\nExamples:\n| name |\n| Ada |\n")
	r, e := engine.RunFile(context.Background(), f, engine.Options{})
	if e != nil || !r.Passed() {
		t.Fatalf("example column not bound: %v %+v", e, r.Scenarios)
	}
}
