package report

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gi-runner/gi/internal/engine"
)

func TestCucumberJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cucumber.json")
	doc := Document{Features: []engine.FileResult{{
		Path: "users.feature",
		Name: "Users",
		Scenarios: []engine.ScenarioResult{{
			Name: "get", Line: 4, Tags: []string{"smoke"}, Passed: true,
			Steps: []engine.StepResult{{Keyword: "*", Text: "status 200", Line: 5, Passed: true, Duration: time.Millisecond}},
		}},
	}}}
	if err := WriteCucumber(path, doc); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got []map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0]["name"] != "Users" {
		t.Fatalf("%s", raw)
	}
	elements, _ := got[0]["elements"].([]any)
	scenario := cucumberValue[map[string]any](t, elements[0])
	steps := cucumberValue[[]any](t, scenario["steps"])
	step, _ := steps[0].(map[string]any)
	result, _ := step["result"].(map[string]any)
	duration, ok := result["duration"].(float64)
	if result["status"] != "passed" || !ok || duration != float64(time.Millisecond) {
		t.Fatalf("%s", raw)
	}
}

func TestCucumberScenarioOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name      string
		sc        engine.ScenarioResult
		wantStep  string
		wantAfter bool
	}{
		{"expected failure", engine.ScenarioResult{Passed: true, Tags: []string{"fail"}, Steps: []engine.StepResult{{Passed: false, Error: "expected assertion"}}}, "passed", false},
		{"unexpected pass", engine.ScenarioResult{Passed: false, Error: "scenario tagged @fail passed unexpectedly", Steps: []engine.StepResult{{Passed: true}}}, "passed", true},
		{"scenario failure without steps", engine.ScenarioResult{Passed: false, Error: "setup failed"}, "", true},
		{"skipped", engine.ScenarioResult{Passed: true, Skipped: true, Steps: []engine.StepResult{{Passed: false}}}, "skipped", false},
		{"assertion failure", engine.ScenarioResult{Passed: false, Steps: []engine.StepResult{{Passed: false, Error: "assertion"}}}, "failed", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := Document{Features: []engine.FileResult{{Scenarios: []engine.ScenarioResult{tc.sc}}}}
			features := cucumberOf(doc)
			elements := cucumberValue[[]any](t, features[0]["elements"])
			scenario := cucumberValue[map[string]any](t, elements[0])
			steps := cucumberValue[[]any](t, scenario["steps"])
			if tc.wantStep != "" {
				result := cucumberValue[map[string]any](t, cucumberValue[map[string]any](t, steps[0])["result"])
				if result["status"] != tc.wantStep {
					t.Fatalf("result: %#v", result)
				}
				if tc.wantStep != "failed" && result["error_message"] != nil {
					t.Fatal("successful or skipped result includes an error")
				}
			}
			after := cucumberValue[[]any](t, scenario["after"])
			if (len(after) > 0) != tc.wantAfter {
				t.Fatalf("after: %#v", after)
			}
			if tc.wantAfter {
				result := cucumberValue[map[string]any](t, cucumberValue[map[string]any](t, after[0])["result"])
				if result["status"] != "failed" || result["error_message"] != tc.sc.Error {
					t.Fatalf("after result: %#v", result)
				}
			}
		})
	}
}

func cucumberValue[T any](t *testing.T, raw any) T {
	t.Helper()
	got, ok := raw.(T)
	if !ok {
		t.Fatalf("unexpected Cucumber value type: %T", raw)
	}
	return got
}
