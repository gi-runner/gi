package engine_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gi-runner/gi/internal/engine"
	"github.com/gi-runner/gi/internal/feature"
	"github.com/gi-runner/gi/internal/report"
)

func TestReturnedClosureSemantics(t *testing.T) {
	for _, call := range []string{"call read('helper.feature')", "callonce read('helper.feature')", "karate.callSingle('helper.feature')"} {
		t.Run(call, func(t *testing.T) {
			dir := t.TempDir()
			helper := `Feature: helpers
Scenario: helper
* def n = 0
* def nullable = null
* def increment = function(x){ return x + 1 }
* def next = function(){ n = increment(n); return n }
* def bump = function(){ n++; return n }
* def get = function(){ return n }
* def bag = { n: 0 }
* def grow = function(){ bag.n++; return bag.n }
* def xs = []
* def push = function(){ xs.push(xs.length + 1); return xs.length }
* def returnsNull = function(){ return null }
* def returnsBox = function(){ return { a: null, b: 1 } }
* def isNull = function(x){ return x === null && nullable === null }
* def isUndefined = function(x){ return x === undefined }
* def make = function(){ let n = 0; return function(){ return ++n } }
* def lexical = make()
* def current = function(){ return karate.info.scenarioName }
* def nested = { check: function(x){ return x === null } }
`
			if err := os.WriteFile(filepath.Join(dir, "helper.feature"), []byte(helper), 0600); err != nil {
				t.Fatal(err)
			}
			main := `Feature: consumer
Scenario: capture
* def result = ` + call + `
* match result.next() == 1
Scenario: consume
* def n = 50
* def result = ` + call + `
* match result.next() == ` + second(call) + `
* match result.bump() == ` + third(call) + `
* match result.get() == ` + third(call) + `
* match n == 50
* match result.grow() == 1
* match result.grow() == 2
* match result.push() == 1
* match result.push() == 2
* assert result.returnsNull() == null
* def box = result.returnsBox()
* assert box.a == null
* match box.b == 1
* match result.isNull(null) == true
* match result.isUndefined() == true
* match result.isUndefined(null) == false
* match result.lexical() == 1
* match result.lexical() == 2
* match result.current() == 'consume'
* match result.nested.check(null) == true
`
			runRelease(t, filepath.Join(dir, "main.feature"), main, engine.Options{Jobs: 1})
			same := `Feature: together
Scenario: one runtime
* def n = 50
* def result = ` + call + `
* match result.next() == 1
* match result.bump() == 2
* match n == 50
* match result.grow() == 1
* match result.push() == 1
* assert result.returnsNull() == null
* def box = result.returnsBox()
* assert box.a == null
`
			runRelease(t, filepath.Join(dir, "same.feature"), same, engine.Options{Jobs: 1})
		})
	}
}

func second(call string) string {
	if call == "call read('helper.feature')" {
		return "1"
	}
	return "2"
}

func third(call string) string {
	if call == "call read('helper.feature')" {
		return "2"
	}
	return "3"
}

func TestCachedClosureDeadline(t *testing.T) {
	// A subprocess watchdog makes this regression fail instead of hanging CI if
	// a future change stops forwarding cancellation to the origin runtime.
	if os.Getenv("GI_CACHED_DEADLINE_CHILD") != "1" {
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCachedClosureDeadline$") //nolint:gosec // child is this test binary so a missed cancel fails instead of hanging CI
		cmd.Env = append(os.Environ(), "GI_CACHED_DEADLINE_CHILD=1")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("cached cancellation child: %v\n%s", err, out)
		}
		return
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "spin.feature"), []byte("Feature: spin\nScenario: create\n* def spin = function(){ while(true){} }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	f, err := feature.Parse(filepath.Join(dir, "main.feature"), `Feature: deadline
Scenario: create
* def result = callonce read('spin.feature')
Scenario: execute
* def result = callonce read('spin.feature')
* result.spin()
`)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	res, err := engine.RunFile(ctx, f, engine.Options{Jobs: 1})
	if ctx.Err() == nil || (err == nil && res.Passed()) {
		t.Fatalf("deadline not preserved: %#v %v", res, err)
	}
}

func TestHookAttachmentsStayOnTheirHooks(t *testing.T) {
	dir := t.TempDir()
	config := `function fn(){
 ['beforeScenario','afterScenario','afterScenarioOutline','afterFeature'].forEach(function(name){
  karate.configure(name,function(){ karate.embed(name,'text/plain',name) });
 });
 return {};
}`
	if err := os.WriteFile(filepath.Join(dir, "karate-config.js"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	f, err := feature.Parse(filepath.Join(dir, "main.feature"), `Feature: hooks
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
	res, err := engine.RunFile(t.Context(), f, engine.Options{Jobs: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Passed() {
		t.Fatalf("hooks failed: %#v", res)
	}
	counts := map[string]int{}
	for _, sc := range res.Scenarios {
		for _, step := range sc.Steps {
			for _, attachment := range step.Embeds {
				if attachment.Name != step.Text || string(attachment.Data) != step.Text {
					t.Fatalf("misattributed attachment on %q: %#v", step.Text, attachment)
				}
				counts[step.Text]++
			}
		}
	}
	for name, want := range map[string]int{"beforeScenario": 2, "afterScenario": 2, "afterScenarioOutline": 1, "afterFeature": 1} {
		if counts[name] != want {
			t.Errorf("%s attachments: %d, want %d", name, counts[name], want)
		}
	}
}

func TestFailingHookKeepsAttachment(t *testing.T) {
	res, err := engine.RunFile(t.Context(), parse(t, `Feature: evidence
Scenario: failed cleanup
* configure afterScenario = function(){ karate.embed('cleanup evidence','text/plain','cleanup'); throw Error('cleanup failed') }
* def ok = true
`), engine.Options{})
	if err != nil {
		t.Fatal(err)
	}
	sc := res.Scenarios[0]
	if sc.Passed || !strings.Contains(sc.Error, "cleanup failed") {
		t.Fatalf("hook result: %#v", sc)
	}
	last := sc.Steps[len(sc.Steps)-1]
	if last.Text != "afterScenario" || len(last.Embeds) != 1 || string(last.Embeds[0].Data) != "cleanup evidence" {
		t.Fatalf("lost hook attachment: %#v", last)
	}
	path := filepath.Join(t.TempDir(), "report.json")
	if err := report.WriteJSON(path, report.Document{Title: "hooks", Started: time.Now(), Features: []engine.FileResult{res}}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Features []struct {
			Scenarios []struct {
				Steps []struct {
					Text   string `json:"text"`
					Embeds []struct {
						Name string `json:"name"`
						Mime string `json:"mime"`
					} `json:"embeds"`
				} `json:"steps"`
			} `json:"scenarios"`
		} `json:"features"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	steps := doc.Features[0].Scenarios[0].Steps
	got := steps[len(steps)-1]
	if got.Text != "afterScenario" || len(got.Embeds) != 1 || got.Embeds[0].Name != "cleanup" || got.Embeds[0].Mime != "text/plain" {
		t.Fatalf("report dropped hook attachment: %s", raw)
	}
}

func TestStepFailureAttachmentStaysOnTheHook(t *testing.T) {
	res, err := engine.RunFile(t.Context(), parse(t, `Feature: evidence
Scenario: continue
* configure onStepFailure = function(info){ karate.embed('kept','text/plain','kept'); info.proceed() }
* assert false
* karate.embed('step','text/plain','step')
`), engine.Options{})
	if err != nil {
		t.Fatal(err)
	}
	var hook, step *engine.StepResult
	for i := range res.Scenarios[0].Steps {
		sr := &res.Scenarios[0].Steps[i]
		switch sr.Text {
		case "onStepFailure":
			hook = sr
		case "karate.embed('step','text/plain','step')":
			step = sr
		}
	}
	if hook == nil || len(hook.Embeds) != 1 || string(hook.Embeds[0].Data) != "kept" {
		t.Fatalf("hook attachment: %#v", res.Scenarios[0].Steps)
	}
	if step == nil || len(step.Embeds) != 1 || string(step.Embeds[0].Data) != "step" {
		t.Fatalf("step attachment: %#v", res.Scenarios[0].Steps)
	}
}

func TestGetterFailureStaysInsideScenario(t *testing.T) {
	f, err := feature.Parse("getters.feature", `Feature: getters
Scenario: export failure
* def item = {}
* eval item = {get value(){throw Error('getter failed')}}
Scenario: healthy
* assert true
Scenario: file hook export failure
* def item = {}
* configure afterFeature = function(){item = {get value(){throw Error('hook getter failed')}}}
`)
	if err != nil {
		t.Fatal(err)
	}
	result, err := engine.RunFile(t.Context(), f, engine.Options{Jobs: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Scenarios) != 3 || result.Scenarios[0].Passed || !result.Scenarios[1].Passed || result.Scenarios[2].Passed {
		t.Fatalf("getter failures escaped their scenarios: %+v", result.Scenarios)
	}
	if !strings.Contains(result.Scenarios[0].Error, "getter failed") || !strings.Contains(result.Scenarios[2].Error, "hook getter failed") {
		t.Fatalf("lost getter errors: %+v", result.Scenarios)
	}
}
