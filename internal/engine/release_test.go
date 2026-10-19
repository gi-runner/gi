package engine_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gi-runner/gi/internal/engine"
	"github.com/gi-runner/gi/internal/feature"
	"github.com/gi-runner/gi/internal/httpx"
	"github.com/gi-runner/gi/internal/suite"
)

func runRelease(t *testing.T, path, source string, opt engine.Options) {
	t.Helper()
	f, err := feature.Parse(path, source)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	res, err := engine.RunFile(ctx, f, opt)
	if err != nil {
		t.Fatal(err)
	}
	for _, sc := range res.Scenarios {
		if !sc.Passed {
			t.Errorf("%s: %s", sc.Name, sc.Error)
		}
	}
}

func TestKarateCoreConformance(t *testing.T) {
	tests := map[string]string{
		"statements and helpers": `* def obj = { n: 1 }
* eval obj.n = 2
* obj.n++
* match obj.n == 3
* match karate.map([3,4], function(x,i){return x+i}) == [3,5]
* match karate.range(3,1) == [3,2,1]
* match karate.append([1], [2,3], 4) == [1,2,3,4]
* match karate.merge({ a: { x: 1 } }, { a: { y: 2 } }) == { a: { y: 2 } }
* match karate.get('obj.n', 9) == 3
* match karate.get('obj.missing', 9) == 9
* karate.set('obj', 'n', 7)
* match obj.n == 7
* karate.remove('obj.n')
* match obj.n == '#notpresent'`,
		"string and bytes": `* string s = { a: 1 }
* json j = s
* match j == { a: 1 }
* bytes b = 'abc'
* string text = b
* match text == 'abc'`,
		"set table": `* set user
| path | value |
| name | 'Ada' |
| age | 36 |
* match user == { name: 'Ada', age: 36 }
* set users
| path | 0 | 1 |
| name | 'Ada' | 'Grace' |
* match users == [{name:'Ada'},{name:'Grace'}]`,
		"call function": `* def twice = function(x){return x * 2}
* def result = call twice 4
* match result == 8`,
		"empty match each": `* configure matchEachEmptyAllowed = true
* match each [] == '#number'`,
	}
	for name, steps := range tests {
		t.Run(name, func(t *testing.T) {
			runRelease(t, "core.feature", "Feature: compatibility\nScenario: "+name+"\n"+steps, engine.Options{})
		})
	}
}

func TestKarateTypedOutlines(t *testing.T) {
	runRelease(t, "rows.feature", `Feature: typed rows
Scenario Outline: typed
* match __num == 0
* match count == 2
* match __row.item == { id: 1 }
Examples:
| count! | item! |
| 2 | { id: 1 } |
`, engine.Options{})
	runRelease(t, "dynamic.feature", `Feature: dynamic rows
Scenario Outline: dynamic
* match count == 2
* match item.id == 1
* match __num == 0
Examples:
| data |
`, engine.Options{Vars: map[string]any{"data": []any{map[string]any{"count": 2, "item": map[string]any{"id": 1}}}}})
}

func TestKarateFilesAndCalls(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"karate-config.js": `function fn(){ return { base: 3 }; }`,
		"data.json":        `{"n":"#(base)","omit":"##(null)"}`,
		"data.yaml":        "name: Ada\n",
		"data.csv":         "name,age\nAda,36\n",
		"fn.js":            `function(x){ return x + 1 }`,
		"child.feature":    "Feature: child\n@chosen\nScenario: child\n* def output = name + __loop\n@other\nScenario: excluded\n* assert false\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(dir, "main.feature")
	runRelease(t, path, `Feature: files
Scenario: files
* match base == 3
* match read('this:data.json') == { n: 3 }
* match read('data.yaml') == { name: 'Ada' }
* match read('data.csv') == [{ name: 'Ada', age: '36' }]
* def result = call read('fn.js') 4
* match result == 5
* def rows = call read('child.feature@chosen') [{name:'Ada'}, {name:'Grace'}]
* match rows[*].output == ['Ada0','Grace1']
`, engine.Options{Suite: suite.Open(path, "")})
}

func TestResponseCookieScope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/set" {
			http.SetCookie(w, &http.Cookie{Name: "secure", Value: "secret", Path: "/", Secure: true})
			http.SetCookie(w, &http.Cookie{Name: "private", Value: "secret", Path: "/private"})
			http.SetCookie(w, &http.Cookie{Name: "normal", Value: "ok", Path: "/"})
			return
		}
		if _, err := r.Cookie("secure"); err == nil {
			t.Error("Secure cookie leaked to HTTP")
		}
		if _, err := r.Cookie("private"); err == nil {
			t.Error("path cookie leaked")
		}
		if _, err := r.Cookie("normal"); err != nil {
			t.Error("normal cookie missing")
		}
	}))
	defer srv.Close()
	// Test a non-loopback origin: Go's jar deliberately treats localhost as secure.
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		t.Fatal("default transport is not an HTTP transport")
	}
	transport := base.Clone()
	transport.Proxy = nil
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, srv.Listener.Addr().String())
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}
	runRelease(t, "cookies.feature", fmt.Sprintf(`Feature: cookies
Scenario: scoped
* url '%s'
* path 'set'
* method get
* match responseCookies.normal.value == 'ok'
* path 'public'
* method get
* status 200
`, "http://cookie.test"), engine.Options{HTTP: httpx.New(client)})
}

func TestHTTPCollectionsAndCallInheritance(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("drop") != "" {
			t.Error("null param sent")
		}
		if len(r.URL.Query()["q"]) != 2 {
			t.Error("list params not repeated")
		}
		if len(r.Header.Values("X-Multi")) != 2 {
			t.Error("multi header missing")
		}
		w.Header().Add("X-Reply", "a")
		w.Header().Add("X-Reply", "b")
	}))
	defer srv.Close()
	dir := t.TempDir()
	child := `Feature: child
Scenario: child
* params {q:['a','b'],drop:null}
* method verb
* match responseHeaders['X-Reply'] == ['a','b']
* match header X-Reply == 'a'
`
	if err := os.WriteFile(filepath.Join(dir, "child.feature"), []byte(child), 0600); err != nil {
		t.Fatal(err)
	}
	runRelease(t, filepath.Join(dir, "main.feature"), fmt.Sprintf(`Feature: HTTP
Scenario: HTTP
* url '%s'
* def verb = 'get'
* header X-Multi = ['one','two']
* call 'child.feature'
`, srv.URL), engine.Options{HTTP: httpx.New(srv.Client())})
}

func TestInvalidValidatorsCannotPassNegation(t *testing.T) {
	for _, step := range []string{"* match 1 != '#? missingFunction()'", "* match each [1] != '#? missingFunction()'", "* match 'a' != '#regex ['"} {
		f, err := feature.Parse("invalid.feature", "Feature: invalid\nScenario: validator\n"+step)
		if err != nil {
			t.Fatal(err)
		}
		res, err := engine.RunFile(t.Context(), f, engine.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if res.Passed() {
			t.Fatalf("invalid validator passed: %s", step)
		}
	}
}

func TestExcludedDynamicExamplesDoNotEvaluate(t *testing.T) {
	for _, tc := range []struct{ tags, filter, env string }{
		{"@ignore", "", ""},
		{"@other", "@chosen", ""},
		{"@env=prod", "", "dev"},
	} {
		t.Run(tc.tags, func(t *testing.T) {
			f, err := feature.Parse("excluded.feature", "Feature: excluded\n"+tc.tags+"\nScenario Outline: excluded\n* assert false\nExamples:\n| missingFunction() |\n")
			if err != nil {
				t.Fatal(err)
			}
			result, err := engine.RunFile(t.Context(), f, engine.Options{Tag: tc.filter, Env: tc.env})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Scenarios) != 1 || !result.Scenarios[0].Skipped {
				t.Fatalf("excluded outline ran: %+v", result)
			}
		})
	}
}

func TestSharedCallStateAndCacheReplay(t *testing.T) {
	dir := t.TempDir()
	child := `Feature: shared settings
Scenario: set settings
* url 'http://shared.example.test'
* header X-Shared = 'child'
* configure followRedirects = false
* configure readTimeout = 1234
* set inherited.n = 2
* def output = 'done'
`
	if err := os.WriteFile(filepath.Join(dir, "child.feature"), []byte(child), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "main.feature")
	f, err := feature.Parse(path, `Feature: replay
Background:
* def inherited = {n:1}
* def unrelatedFunction = function(){ return 1 }
Scenario: first
* callonce 'child.feature'
* match inherited.n == 2
* method get
Scenario: second
* callonce 'child.feature'
* match inherited.n == 2
* method get
Scenario: explicit shared
* karate.call(true, 'child.feature')
* match output == 'done'
* match inherited.n == 2
* method get
Scenario: isolated
* def result = call 'child.feature'
* match inherited.n == 1
* match result.output == 'done'
`)
	if err != nil {
		t.Fatal(err)
	}
	var calls int
	sender := releaseSenderFunc(func(ctx context.Context, call engine.Call) (engine.Reply, error) {
		calls++
		if call.URL != "http://shared.example.test" || http.Header(call.Header).Get("X-Shared") != "child" || call.Follow {
			t.Errorf("shared settings lost: %+v", call)
		}
		if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 1300*time.Millisecond {
			t.Error("shared timeout lost")
		}
		return engine.Reply{Status: 200}, nil
	})
	result, err := engine.RunFile(t.Context(), f, engine.Options{HTTP: sender})
	if err != nil || !result.Passed() {
		t.Fatalf("%s (%v)", scenarioErr(result), err)
	}
	if calls != 3 {
		t.Fatalf("HTTP calls: %d", calls)
	}
}

func TestCalledSelectorsAndArguments(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.feature")
	source := `Feature: calls
@ignore @helper
Scenario: helper
* def result = __arg.n + 1
@ignore @excluded
Scenario Outline: never evaluated
* assert false
Examples:
| missingFunction() |
Scenario: caller
* def result = call '@helper' {n:1}
* match result.result == 2
`
	if err := os.WriteFile(path, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	f, err := feature.ParseFile(path)
	if err != nil {
		t.Fatal(err)
	}
	result, err := engine.RunFile(t.Context(), f, engine.Options{})
	if err != nil || !result.Passed() {
		t.Fatalf("%s (%v)", scenarioErr(result), err)
	}
}

func TestCallSingleJSAndCacheIsolation(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "fn.js"), []byte(`function(arg){ return { nested:{ n:arg.n } } }`), 0600); err != nil {
		t.Fatal(err)
	}
	runRelease(t, filepath.Join(dir, "main.feature"), `Feature: singleton
Scenario: cache
* def first = karate.callSingle('fn.js', {n:1})
* first.nested.n = 2
* def second = karate.callSingle('fn.js', {n:99})
* match second.nested.n == 1
`, engine.Options{})
}

type releaseSenderFunc func(context.Context, engine.Call) (engine.Reply, error)

func (f releaseSenderFunc) Send(ctx context.Context, call engine.Call) (engine.Reply, error) {
	return f(ctx, call)
}

func TestStoredFeatureReferenceAndJSObjectCall(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"child.feature": "Feature: child\nScenario: child\n* def answer = 42\n",
		"object.js":     "({answer:42})",
		"recursive.js":  "function(){ return karate.callSingle('recursive.js') }",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	runRelease(t, filepath.Join(dir, "main.feature"), `Feature: files
Scenario: files
* def helper = read('child.feature')
* def result = call helper
* match result.answer == 42
* def obj = karate.call('object.js')
* match obj.answer == 42
`, engine.Options{})
	f, err := feature.Parse(filepath.Join(dir, "main.feature"), "Feature: cycle\nScenario: cycle\n* def result = karate.callSingle('recursive.js')\n")
	if err != nil {
		t.Fatal(err)
	}
	result, err := engine.RunFile(t.Context(), f, engine.Options{})
	if err != nil || result.Passed() {
		t.Fatalf("cycle passed: %+v %v", result, err)
	}
}

func TestHeaderOverlayPreservesListsAndNull(t *testing.T) {
	sender := releaseSenderFunc(func(_ context.Context, call engine.Call) (engine.Reply, error) {
		h := http.Header(call.Header)
		if len(h.Values("X-Multi")) != 2 || h.Get("X-Drop") != "" {
			t.Errorf("header overlay: %v", h)
		}
		return engine.Reply{Status: http.StatusOK}, nil
	})
	runRelease(t, "headers.feature", `Feature: headers
Scenario: headers
* url 'http://headers.example.test'
* header X-Drop = 'remove'
* configure headers = function(){ return {'X-Multi':['one','two'], 'X-Drop':null} }
* method get
`, engine.Options{HTTP: sender})
}
