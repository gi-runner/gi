package report_test

import (
	"html"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gi-runner/gi/internal/engine"
	"github.com/gi-runner/gi/internal/feature"
	"github.com/gi-runner/gi/internal/load"
	"github.com/gi-runner/gi/internal/report"
)

func TestWritesHTMLJSONAndJUnit(t *testing.T) {
	doc := report.Document{
		Mode:     "functional",
		Title:    "gi functional",
		Started:  time.Now(),
		Duration: 20 * time.Millisecond,
		Features: []engine.FileResult{{
			Path: "users.feature",
			Name: "Users API",
			Scenarios: []engine.ScenarioResult{{
				Name: "create", Passed: true, Duration: 10 * time.Millisecond,
				Steps: []engine.StepResult{{
					Keyword: "*", Text: "method post", Passed: true,
					HTTP: &engine.Exchange{
						Method: "POST", URL: "http://example/users", Status: 201,
						RequestHeaders:  map[string]string{"Content-Type": "application/json"},
						RequestBody:     `{"name":"Ada"}`,
						ResponseHeaders: map[string]string{"Content-Type": "application/json"},
						ResponseBody:    `{"id":"1"}`,
					},
				}},
			}},
		}},
		Load: &load.Result{
			Profile: "constant users 2",
			VUs:     2, Iterations: 10, Requests: 20, Elapsed: time.Second, Passed: true,
			Overall:    load.Stats{P50: time.Millisecond, P95: 2 * time.Millisecond, P99: 3 * time.Millisecond},
			Endpoints:  []load.Endpoint{{Name: "POST /users", Count: 10, Stats: load.Stats{P95: 2 * time.Millisecond}}},
			Thresholds: []load.Threshold{{Text: "p95 < 200ms", Passed: true, Actual: "2ms"}},
		},
	}
	dir := t.TempDir()
	if err := report.WriteHTML(filepath.Join(dir, "r.html"), doc); err != nil {
		t.Fatal(err)
	}
	if err := report.WriteJSON(filepath.Join(dir, "r.json"), doc); err != nil {
		t.Fatal(err)
	}
	if err := report.WriteJUnit(filepath.Join(dir, "r.xml"), doc); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "r.html"))
	if err != nil {
		t.Fatal(err)
	}
	page := string(b)
	for _, want := range []string{"POST /users", "create", "constant users 2", "Content-Type: application/json", html.EscapeString("\"name\": \"Ada\"")} {
		if !strings.Contains(page, want) {
			t.Fatalf("report missing %q", want)
		}
	}
}

func TestCallTreeAndTimeline(t *testing.T) {
	start := time.Now()
	doc := report.Document{
		Mode: "functional", Title: "gi functional", Started: start, Duration: 40 * time.Millisecond,
		Features: []engine.FileResult{{
			Name: "Outer",
			Scenarios: []engine.ScenarioResult{{
				Name: "go", Passed: true, Started: start.Add(5 * time.Millisecond), Duration: 20 * time.Millisecond,
				Steps: []engine.StepResult{{
					Keyword: "*", Text: "call read('inner.feature')", Passed: true,
					Calls: []engine.StepResult{{Keyword: "*", Text: "def n = 1", Passed: true}},
				}},
			}},
		}},
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "r.html")
	if err := report.WriteHTML(path, doc); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	page := string(b)
	for _, want := range []string{"Timeline", "Outer · go", "def n = 1", "call read(&#39;inner.feature&#39;)"} {
		if !strings.Contains(page, want) {
			t.Fatalf("report missing %q", want)
		}
	}
}

func TestJUnitKeepsIterationFailuresWhenThresholdsPass(t *testing.T) {
	path := filepath.Join(t.TempDir(), "load.xml")
	doc := report.Document{Mode: "load", Load: &load.Result{
		Requests: 10, Iterations: 5, FailedIterations: 1, Passed: false,
		Thresholds: []load.Threshold{{Text: "p95 < 1s", Passed: true, Actual: "1ms"}},
	}}
	if err := report.WriteJUnit(path, doc); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "<failure") {
		t.Fatalf("failed load run reported success: %s", raw)
	}
}

func TestReportDisabledRemovesSensitiveDetails(t *testing.T) {
	f, err := feature.Parse("hidden.feature", `Feature: redaction
@report=false
Scenario: hidden
* def secret = 'private-audit-value'
* match secret == 'wrong'
`)
	if err != nil {
		t.Fatal(err)
	}
	result, err := engine.RunFile(t.Context(), f, engine.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed() {
		t.Fatal("hidden assertion did not fail")
	}
	doc := report.Document{Features: []engine.FileResult{result}}
	dir := t.TempDir()
	for _, writer := range []struct {
		name  string
		write func(string, report.Document) error
	}{{"html", report.WriteHTML}, {"json", report.WriteJSON}, {"xml", report.WriteJUnit}} {
		path := filepath.Join(dir, "report."+writer.name)
		if err := writer.write(path, doc); err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "private-audit-value") {
			t.Fatalf("%s report leaked hidden data", writer.name)
		}
	}
}

func TestEndpointAggregationIsVisibleInReports(t *testing.T) {
	result := load.Result{EndpointLimit: 2, EndpointOverflow: 5, Requests: 7, Endpoints: []load.Endpoint{{Name: "[other endpoints]", Count: 5}}}
	doc := report.Document{Mode: "load", Load: &result}
	dir := t.TempDir()
	htmlPath, jsonPath := filepath.Join(dir, "report.html"), filepath.Join(dir, "report.json")
	if err := report.WriteHTML(htmlPath, doc); err != nil {
		t.Fatal(err)
	}
	if err := report.WriteJSON(jsonPath, doc); err != nil {
		t.Fatal(err)
	}
	html, err := os.ReadFile(htmlPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(html), "5 requests aggregated") || !strings.Contains(string(html), "[other endpoints]") {
		t.Fatal("HTML hid endpoint aggregation")
	}
	raw, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"endpointLimit": 2`) || !strings.Contains(string(raw), `"endpointOverflow": 5`) {
		t.Fatal("JSON hid endpoint aggregation")
	}
}
