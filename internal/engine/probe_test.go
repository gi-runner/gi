package engine_test

import (
	"context"
	"net"
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
)

func TestBlockedStepsObserveCancellation(t *testing.T) {
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	tests := []struct {
		name string
		src  string
		opt  engine.Options
	}{
		{name: "pause", src: "Feature: pause\nScenario: wait\n* karate.pause(30000)\n"},
		{name: "listen", src: "Feature: listen\nScenario: wait\n* def got = karate.listen(30000)\n"},
		{name: "http", src: "Feature: http\nScenario: wait\n* karate.waitForHttp(hangUrl)\n", opt: engine.Options{Vars: map[string]any{"hangUrl": "http://" + ln.Addr().String()}}},
		{name: "retry", src: "Feature: retry\nScenario: wait\n* url baseUrl\n* configure retry = { count: 4, interval: 5000 }\n* retry until responseStatus == 200\n* method get\n", opt: engine.Options{HTTP: httpx.New(srv.Client()), Vars: map[string]any{"baseUrl": srv.URL}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := feature.Parse(tt.name+".feature", tt.src)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
			defer cancel()
			got := make(chan error, 1)
			go func() {
				res, err := engine.RunFile(ctx, f, tt.opt)
				if err != nil {
					got <- err
					return
				}
				if res.Passed() {
					got <- errScenarioPassed
					return
				}
				got <- nil
			}()
			select {
			case err := <-got:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("blocked step ignored cancellation")
			}
		})
	}
}

var errScenarioPassed = stringError("scenario passed after its deadline")

type stringError string

func (e stringError) Error() string { return string(e) }

func TestSignalNullAndCopy(t *testing.T) {
	res, err := engine.RunFile(t.Context(), parse(t, `
Feature: signal
Scenario: null stays null
* karate.signal(null)
* def got = karate.listen(200)
* assert got == null
Scenario: caller mutation does not change the queued value
* def event = { id: 1 }
* karate.signal(event)
* event.id = 9
* def got = karate.listen(200)
* match got.id == 1
`), engine.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Passed() {
		t.Fatalf("%s / %s", res.Scenarios[0].Error, res.Scenarios[1].Error)
	}
}

func TestOneSignalWakesOneListener(t *testing.T) {
	res, err := engine.RunFile(t.Context(), parse(t, `
Feature: fanout
Scenario: first
* def got = karate.listen(800)
Scenario: second
* def got = karate.listen(800)
Scenario: send
* karate.pause(20)
* karate.signal('only')
`), engine.Options{Jobs: 3})
	if err != nil {
		t.Fatal(err)
	}
	var passed, timedOut int
	for _, sc := range res.Scenarios {
		switch {
		case sc.Name == "send" && sc.Passed:
			passed++
		case sc.Passed:
			passed++
		case strings.Contains(sc.Error, "listen timed out"):
			timedOut++
		default:
			t.Errorf("%s: %s", sc.Name, sc.Error)
		}
	}
	if passed != 2 || timedOut != 1 {
		t.Fatalf("passed=%d timed out=%d %#v", passed, timedOut, res.Scenarios)
	}
}

func TestEmbedStaysWithTheStepThatCreatedIt(t *testing.T) {
	dir := t.TempDir()
	child := "Feature: child\nScenario: child\n* karate.embed('nested','text/plain','nested')\n* assert false\n"
	if err := os.WriteFile(filepath.Join(dir, "child.feature"), []byte(child), 0o600); err != nil {
		t.Fatal(err)
	}
	note := "Feature: note\nScenario: note\n* def note = function(){ karate.embed('from-fn','text/plain','from-fn'); return true }\n"
	if err := os.WriteFile(filepath.Join(dir, "note.feature"), []byte(note), 0o600); err != nil {
		t.Fatal(err)
	}
	failed := parse(t, `
Feature: evidence
Scenario: expression
* assert (function(){ karate.embed('kept','text/plain','kept'); return false })()
`)
	failed.Path = filepath.Join(dir, "failed.feature")
	res, err := engine.RunFile(t.Context(), failed, engine.Options{})
	if err != nil {
		t.Fatal(err)
	}
	step := res.Scenarios[0].Steps[0]
	if step.Passed || len(step.Embeds) != 1 || string(step.Embeds[0].Data) != "kept" {
		t.Fatalf("failed step lost its embed: %#v", step)
	}

	caller := `Feature: caller
Background:
* karate.embed('background','text/plain','background')
Scenario: call
* def result = call read('child.feature')
Scenario: cached
* def result = callonce read('note.feature')
* assert result.note()
`
	f, err := feature.Parse(filepath.Join(dir, "caller.feature"), caller)
	if err != nil {
		t.Fatal(err)
	}
	res, err = engine.RunFile(t.Context(), f, engine.Options{Jobs: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Scenarios) != 2 {
		t.Fatalf("scenarios: %#v", res.Scenarios)
	}
	background := embedNamed(res.Scenarios[0].Steps, "background")
	if background == nil || background.Text == "def result = call read('child.feature')" {
		t.Fatalf("background embed: %#v", res.Scenarios[0].Steps)
	}
	callStep := stepNamed(res.Scenarios[0].Steps, "def result = call read('child.feature')")
	if callStep == nil || len(callStep.Embeds) != 0 || embedNamed(callStep.Calls, "nested") == nil {
		t.Fatalf("call embed: %#v", res.Scenarios[0].Steps)
	}
	noteStep := stepNamed(res.Scenarios[1].Steps, "assert result.note()")
	if noteStep == nil || len(noteStep.Embeds) != 1 || string(noteStep.Embeds[0].Data) != "from-fn" {
		t.Fatalf("cached function embed: %#v", res.Scenarios[1].Steps)
	}
}

func TestCallArgumentPreservesNull(t *testing.T) {
	dir := t.TempDir()
	child := "Feature: child\nScenario: child\n* assert __arg.value == null\n* assert __arg.present == true\n* def echoed = __arg.value\n"
	if err := os.WriteFile(filepath.Join(dir, "child.feature"), []byte(child), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := feature.Parse(filepath.Join(dir, "main.feature"), `Feature: args
Scenario: null field
* def result = call read('child.feature') { value: null, present: true }
* assert result.echoed == null
`)
	if err != nil {
		t.Fatal(err)
	}
	res, err := engine.RunFile(t.Context(), f, engine.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Passed() {
		t.Fatal(res.Scenarios[0].Error)
	}
}

func TestCachedCallbackKeepsLexicalState(t *testing.T) {
	dir := t.TempDir()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/check" && r.Header.Get("X-Token") != "cached-live" {
			t.Errorf("header: %s", r.Header.Get("X-Token"))
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	child := `Feature: login
Scenario: login
* def token = 'cached'
* configure headers = (function(){ var suffix = '-live'; return function(){ return { 'X-Token': token + suffix } } })()
* configure afterScenario = (function(){ var label = 'closed'; return function(){ karate.embed(label,'text/plain',label) } })()
`
	if err := os.WriteFile(filepath.Join(dir, "login.feature"), []byte(child), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := feature.Parse(filepath.Join(dir, "main.feature"), `Feature: cached callbacks
Background:
* url baseUrl
Scenario: first
* callonce 'login.feature'
* path 'check'
* method get
Scenario: second
* callonce 'login.feature'
* path 'check'
* method get
`)
	if err != nil {
		t.Fatal(err)
	}
	res, err := engine.RunFile(t.Context(), f, engine.Options{HTTP: httpx.New(srv.Client()), Vars: map[string]any{"baseUrl": srv.URL}, Jobs: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Passed() {
		t.Fatalf("%s / %s", res.Scenarios[0].Error, res.Scenarios[1].Error)
	}
	for _, sc := range res.Scenarios {
		if embedNamed(sc.Steps, "closed") == nil || embedNamed(sc.Steps, "closed").Text != "afterScenario" {
			t.Fatalf("hook lost its lexical label: %#v", sc.Steps)
		}
	}
}

func TestCachedFunctionCanSignal(t *testing.T) {
	dir := t.TempDir()
	child := "Feature: ping\nScenario: ping\n* def send = function(){ karate.signal('ping') }\n* def wait = function(){ return karate.listen(1000) }\n"
	if err := os.WriteFile(filepath.Join(dir, "ping.feature"), []byte(child), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := feature.Parse(filepath.Join(dir, "main.feature"), `Feature: bus
Scenario: wait
* def box = callonce read('ping.feature')
* def got = box.wait()
* match got == 'ping'
Scenario: send
* karate.pause(30)
* def box = callonce read('ping.feature')
* box.send()
`)
	if err != nil {
		t.Fatal(err)
	}
	res, err := engine.RunFile(t.Context(), f, engine.Options{Jobs: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Passed() {
		t.Fatalf("%s / %s", res.Scenarios[0].Error, res.Scenarios[1].Error)
	}
}

func TestCachedPauseLetsAnotherCallRun(t *testing.T) {
	dir := t.TempDir()
	child := "Feature: hold\nScenario: hold\n* def hold = function(){ karate.signal('started'); karate.pause(800) }\n* def touch = function(x){ return x }\n"
	if err := os.WriteFile(filepath.Join(dir, "hold.feature"), []byte(child), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := feature.Parse(filepath.Join(dir, "main.feature"), `Feature: pause
Scenario: hold
* def box = callonce read('hold.feature')
* box.hold()
Scenario: touch
* def started = karate.listen(1000)
* match started == 'started'
* def box = callonce read('hold.feature')
* match box.touch(null) == null
`)
	if err != nil {
		t.Fatal(err)
	}
	res, err := engine.RunFile(t.Context(), f, engine.Options{Jobs: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Passed() {
		t.Fatalf("%s / %s", res.Scenarios[0].Error, res.Scenarios[1].Error)
	}
	if res.Scenarios[1].Duration > 400*time.Millisecond {
		t.Fatalf("cached pause blocked the other scenario for %s", res.Scenarios[1].Duration)
	}
}

func TestCachedWaitLetsAnotherCallRun(t *testing.T) {
	dir := t.TempDir()
	child := "Feature: hold\nScenario: hold\n* def hold = function(){ karate.signal('started'); karate.waitForPort('127.0.0.1', 1, 800, 40) }\n* def touch = function(){ return true }\n"
	if err := os.WriteFile(filepath.Join(dir, "hold.feature"), []byte(child), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := feature.Parse(filepath.Join(dir, "main.feature"), `Feature: wait
Scenario: hold
* def box = callonce read('hold.feature')
* box.hold()
Scenario: touch
* def started = karate.listen(1000)
* match started == 'started'
* def box = callonce read('hold.feature')
* box.touch()
`)
	if err != nil {
		t.Fatal(err)
	}
	res, err := engine.RunFile(t.Context(), f, engine.Options{Jobs: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Scenarios[1].Passed {
		t.Fatal(res.Scenarios[1].Error)
	}
	if res.Scenarios[1].Duration > 400*time.Millisecond {
		t.Fatalf("cached waitForPort blocked the other scenario for %s", res.Scenarios[1].Duration)
	}
	if res.Scenarios[0].Passed {
		t.Fatal("waitForPort reached a closed port")
	}
}

func TestOutlineHookRunsOnceWhenScenariosOverlap(t *testing.T) {
	dir := t.TempDir()
	config := `function fn(){
 ['afterScenarioOutline','afterFeature'].forEach(function(name){
  karate.configure(name,function(){ karate.embed(name,'text/plain',name) });
 });
 return {};
}`
	if err := os.WriteFile(filepath.Join(dir, "karate-config.js"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := feature.Parse(filepath.Join(dir, "outline.feature"), `Feature: hooks
Scenario Outline: row <id>
* def value = <id>
Examples:
| id |
| 1 |
| 2 |
`)
	if err != nil {
		t.Fatal(err)
	}
	res, err := engine.RunFile(t.Context(), f, engine.Options{Jobs: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Passed() {
		t.Fatalf("outline failed: %#v", res.Scenarios)
	}
	counts := map[string]int{}
	for _, sc := range res.Scenarios {
		for _, step := range sc.Steps {
			for _, item := range step.Embeds {
				if item.Name != step.Text {
					t.Fatalf("hook embed on %q: %#v", step.Text, item)
				}
				counts[item.Name]++
			}
		}
	}
	if counts["afterScenarioOutline"] != 1 || counts["afterFeature"] != 1 {
		t.Fatalf("hook counts: %#v steps: %#v", counts, res.Scenarios)
	}
}

func embedNamed(steps []engine.StepResult, name string) *engine.StepResult {
	for i := range steps {
		for _, item := range steps[i].Embeds {
			if item.Name == name {
				return &steps[i]
			}
		}
		if found := embedNamed(steps[i].Calls, name); found != nil {
			return found
		}
	}
	return nil
}

func stepNamed(steps []engine.StepResult, text string) *engine.StepResult {
	for i := range steps {
		if steps[i].Text == text {
			return &steps[i]
		}
		if found := stepNamed(steps[i].Calls, text); found != nil {
			return found
		}
	}
	return nil
}
