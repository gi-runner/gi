package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gi-runner/gi/internal/load"
)

func TestCLIChild(t *testing.T) {
	if os.Getenv("GI_TEST_CLI_CHILD") != "1" {
		return
	}
	for i, arg := range os.Args {
		if arg == "--" {
			os.Exit(Main(os.Args[i+1:]))
		}
	}
	t.Fatal("missing child arguments")
}

func TestCommandExitCodesAndReports(t *testing.T) {
	dir := t.TempDir()
	feature := filepath.Join(dir, "run.feature")
	for _, tc := range []struct {
		name, step string
		code       int
	}{
		{"pass", "* bytes a = 'abc'\n* bytes b = 'abc'\n* match a == b\n* def rows = [{id:1},{id:2}]\n* match rows.filter(x => x.id == 1) == [{id:1}]", 0},
		{"fail", "* assert false", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(feature, []byte("Feature: command\nScenario: run\n"+tc.step+"\n"), 0600); err != nil {
				t.Fatal(err)
			}
			args := make([]string, 0, 10)
			args = append(args, "test", feature)
			reports := map[string]string{"report": "html", "json": "json", "junit": "xml", "cucumber": "json"}
			for flag, ext := range reports {
				args = append(args, "--"+flag, filepath.Join(dir, tc.name+"-"+flag+"."+ext))
			}
			code, out := runCLI(t, args...)
			if code != tc.code {
				t.Fatalf("exit %d, want %d: %s", code, tc.code, out)
			}
			for flag, ext := range reports {
				data, err := os.ReadFile(filepath.Join(dir, tc.name+"-"+flag+"."+ext))
				if err != nil || len(data) == 0 {
					t.Fatalf("%s report: %v", flag, err)
				}
			}
		})
	}
	if code, out := runCLI(t, "test", "--unknown-flag"); code != 2 || !strings.Contains(out, "Usage") {
		t.Fatalf("invalid flags: %d %s", code, out)
	}
}

func runCLI(t *testing.T, args ...string) (int, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(ctx, executable, append([]string{"-test.run=^TestCLIChild$", "--"}, args...)...)
	command.Env = append(os.Environ(), "GI_TEST_CLI_CHILD=1")
	out, err := command.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("CLI hung: %v %s", ctx.Err(), out)
	}
	if err == nil {
		return 0, string(out)
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		t.Fatal(err)
	}
	return exit.ExitCode(), string(out)
}

func TestLoadEndpointLimitFromCLIToReports(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer server.Close()
	dir := t.TempDir()
	featurePath := filepath.Join(dir, "endpoints.feature")
	source := `Feature: endpoint limits
Scenario: unique path
* url baseUrl
* path 'item-' + vu
* method get
* status 200
`
	if err := os.WriteFile(featurePath, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	jsonPath, htmlPath := filepath.Join(dir, "load.json"), filepath.Join(dir, "load.html")
	code, out := runCLI(t, "load", featurePath, "--rate", "50", "--duration", "200ms", "--max-endpoints", "1", "--var", "baseUrl="+server.URL, "--json", jsonPath, "--report", htmlPath)
	if code != 0 {
		t.Fatalf("load exited %d: %s", code, out)
	}
	if !strings.Contains(out, "requests aggregated into [other endpoints]") {
		t.Fatalf("CLI hid endpoint aggregation: %s", out)
	}
	for _, path := range []string{jsonPath, htmlPath} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), "[other endpoints]") {
			t.Fatalf("report omitted overflow endpoint: %s", path)
		}
	}
}

func TestLoadPartsMergeIntoOneRun(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	dir := t.TempDir()
	featurePath := filepath.Join(dir, "users.feature")
	source := "# gi-load: threshold errorRate < 1%\nFeature: parts\nScenario: get\n* url baseUrl\n* method get\n* status 200\n"
	if err := os.WriteFile(featurePath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	var snaps []string
	for i := 1; i <= 2; i++ {
		snap := filepath.Join(dir, fmt.Sprintf("part%d.json", i))
		snaps = append(snaps, snap)
		code, out := runCLI(t, "load", featurePath, "--vus", "2", "--duration", "300ms", "--part", fmt.Sprintf("%d/2", i), "--snapshot", snap, "--var", "baseUrl="+server.URL)
		if code != 0 || !strings.Contains(out, fmt.Sprintf("part=%d/2", i)) {
			t.Fatalf("part %d exit %d\n%s", i, code, out)
		}
	}
	report := filepath.Join(dir, "merged.json")
	code, out := runCLI(t, append([]string{"load", "merge", "--json", report, "--threshold", "p95 < 1s"}, snaps...)...)
	if code != 0 || !strings.Contains(out, "constant users 2 across 2 parts") || !strings.Contains(out, "threshold errorRate < 1%") || !strings.Contains(out, "threshold p95 < 1s") {
		t.Fatalf("merge exit %d\n%s", code, out)
	}
	if _, err := os.Stat(report); err != nil {
		t.Fatal(err)
	}
	if code, out := runCLI(t, "load", "merge", snaps[0]); code != 2 || !strings.Contains(out, "1 snapshots for a profile in 2 parts") {
		t.Fatalf("merged an incomplete set: exit %d\n%s", code, out)
	}
	if code, out := runCLI(t, "load", featurePath, "--vus", "1", "--duration", "1s", "--part", "2/2"); code != 2 || !strings.Contains(out, "part 2 of 2") {
		t.Fatalf("exit %d\n%s", code, out)
	}
	if code, out := runCLI(t, "load", featurePath, "--part", "3/2"); code != 2 || !strings.Contains(out, "--part expects") {
		t.Fatalf("exit %d\n%s", code, out)
	}
}

func TestListenMetricsServesPrometheus(t *testing.T) {
	s := &scrape{}
	s.record("shop", load.Progress{Users: 4, Requests: 20, Failed: 1})
	s.record("spike", load.Progress{Users: 8, Requests: 3, Failed: 0})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	stop, bound, err := listenMetrics(ctx, "127.0.0.1:0", s)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+bound+"/metrics", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	got := string(body)
	for _, want := range []string{`gi_vus{feature="shop"} 4`, `gi_http_reqs{feature="shop"} 20`, `gi_http_req_failed{feature="shop"} 1`, `gi_vus{feature="spike"} 8`} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %s in\n%s", want, got)
		}
	}
}

func TestInitPlanAndDemo(t *testing.T) {
	t.Chdir(t.TempDir())
	code, out := runCLI(t, "init")
	if code != 0 || !strings.Contains(out, "gi mock hello.mock.feature") {
		t.Fatalf("init exit %d\n%s", code, out)
	}
	if code, out = runCLI(t, "init"); code != 2 || !strings.Contains(out, "already exists") {
		t.Fatalf("second init exit %d\n%s", code, out)
	}
	code, out = runCLI(t, "load", "plan", "hello.feature", "--parts", "2", "--start-at", "2026-11-08T16:00:00Z")
	if code != 0 || !strings.Contains(out, "--part 1/2") || !strings.Contains(out, "--part 2/2") || !strings.Contains(out, "gi load merge") {
		t.Fatalf("plan exit %d\n%s", code, out)
	}
	code, out = runCLI(t, "demo")
	if code != 0 || !strings.Contains(out, "demo passed") {
		t.Fatalf("demo exit %d\n%s", code, out)
	}
}

func TestTourExamples(t *testing.T) {
	code, out := runCLI(t, "test", filepath.Join("..", "..", "examples", "tour"))
	if code != 0 {
		t.Fatalf("tour exit %d\n%s", code, out)
	}
}

func TestCommandHelpVersionAndUsage(t *testing.T) {
	for _, tc := range []struct {
		name     string
		args     []string
		code     int
		contains string
	}{
		{"empty", nil, 2, "Usage:"},
		{"root help", []string{"--help"}, 0, "completion"},
		{"test help", []string{"test", "--help"}, 0, "--jobs"},
		{"load help", []string{"help", "load"}, 0, "--max-endpoints"},
		{"mock help", []string{"mock", "-h"}, 0, "--addr"},
		{"check help", []string{"check", "--help"}, 0, "cannot run"},
		{"version command", []string{"version"}, 0, "gi " + version},
		{"version flag", []string{"--version"}, 0, "gi " + version},
		{"short version", []string{"-v"}, 0, "gi " + version},
		{"unknown command", []string{"unknown"}, 2, "unknown command"},
		{"unknown flag", []string{"test", "--unknown"}, 2, "unknown flag"},
		{"invalid duration", []string{"load", "--duration", "oops"}, 2, "invalid argument"},
		{"missing mock path", []string{"mock"}, 2, "accepts 1 arg"},
		{"extra mock path", []string{"mock", "a.feature", "b.feature"}, 2, "accepts 1 arg"},
		{"extra version arg", []string{"version", "extra"}, 2, "unknown command"},
		{"completion", []string{"completion", "bash"}, 0, "__start_gi"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, out := runCLI(t, tc.args...)
			if code != tc.code || !strings.Contains(out, tc.contains) {
				t.Fatalf("exit %d, want %d; want %q in output: %s", code, tc.code, tc.contains, out)
			}
		})
	}
}

func TestSpecJSON(t *testing.T) {
	dir := t.TempDir()
	writeSpec(t, dir, `
### User Story 1 - Health (Priority: P1)

**Acceptance Scenarios**:

1. **Given** the service, **When** GET /health, **Then** status 200
`, `
openapi: 3.0.3
paths:
  /health:
    get:
      responses:
        '200': { description: ok }
`)
	code, out := runCLI(t, "spec", "--json", dir)
	if code != 0 || !strings.Contains(out, `"scenarios": 1`) || !strings.Contains(out, `"notes"`) {
		t.Fatalf("exit %d\n%s", code, out)
	}

	missing := t.TempDir()
	writeSpec(t, missing, `
### User Story 1 - Reminder (Priority: P1)

**Acceptance Scenarios**:

1. **Given** an invoice, **When** POST /invoices/{id}/reminders, **Then** status 201
`, `
openapi: 3.0.3
paths:
  /invoices/{id}/reminders:
    post:
      responses:
        '201': { description: Created }
`)
	code, out = runCLI(t, "spec", "--json", missing)
	if code != 1 || !strings.Contains(out, `"files": []`) || !strings.Contains(out, "add example for id") {
		t.Fatalf("exit %d\n%s", code, out)
	}
}

func writeSpec(t *testing.T, dir, spec, contract string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "spec.md"), []byte(spec), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "openapi.yaml"), []byte(contract), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestCommandRepeatedVariablesAndTerminator(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "run.feature")
	source := `Feature: CLI variables
Scenario: values survive parsing
* match csv == 'a,b'
* match choice == 'last'
* match negative == '-1'
`
	if err := os.WriteFile(path, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	code, out := runCLI(t, "test", "--var", "choice=first", path, "--var", "csv=a,b", "--var", "choice=last", "--var", "negative=-1")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	// Run discovery on a relative path starting with '-' after the terminator.
	name := "-special.feature"
	if err := os.WriteFile(filepath.Join(dir, name), []byte("Feature: terminator\nScenario: run\n* assert true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	code, out = runCLI(t, "test", "--jobs", "1", "--", name)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
}

func TestNamedProfilesShareOneWindow(t *testing.T) {
	var mu sync.Mutex
	times := map[string]struct {
		first, last time.Time
		n           int
	}{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		h := times[r.URL.Path]
		now := time.Now()
		if h.n == 0 {
			h.first = now
		}
		h.last = now
		h.n++
		times[r.URL.Path] = h
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	path := filepath.Join(dir, "shop.feature")
	source := `# gi-load: name=soak vus=2 duration=1200ms
# gi-load: name=spike vus=2 duration=800ms start=400ms
Feature: shop
@load=soak
Scenario: browse
* url baseUrl
* path 'soak'
* method get
* status 200
@load=spike
Scenario: checkout
* url baseUrl
* path 'spike'
* method get
* status 200
`
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out := runCLI(t, "load", path, "--var", "baseUrl="+srv.URL)
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, out)
	}
	mu.Lock()
	defer mu.Unlock()
	soak, spike := times["/soak"], times["/spike"]
	if soak.n == 0 || spike.n == 0 {
		t.Fatalf("requests soak=%d spike=%d", soak.n, spike.n)
	}
	gap := spike.first.Sub(soak.first)
	if gap < 250*time.Millisecond || gap > 900*time.Millisecond {
		t.Fatalf("spike started %s after soak", gap)
	}
	if !soak.last.After(spike.first) {
		t.Fatalf("spike began after soak ended: soak %s-%s spike %s", soak.first, soak.last, spike.first)
	}
}

func TestSpecFeatureKeepsScenarioOrder(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "reminder.feature")
	plain := filepath.Join(dir, "plain.feature")
	body := "# gi-spec: reminder\nFeature: Reminder\nScenario: one\n  * method get\n"
	if err := os.WriteFile(spec, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plain, []byte("Feature: Plain\nScenario: one\n  * method get\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := testJobs([]string{spec}, 0); got != 1 {
		t.Fatalf("spec jobs = %d", got)
	}
	if got := testJobs([]string{spec}, 4); got != 4 {
		t.Fatalf("explicit jobs = %d", got)
	}
	want := runtime.GOMAXPROCS(0)
	if got := testJobs([]string{plain}, 0); got != want {
		t.Fatalf("plain jobs = %d, want %d", got, want)
	}
	if got := testJobs([]string{spec, plain}, 0); got != want {
		t.Fatalf("mixed jobs = %d, want %d", got, want)
	}
}

func TestOpenRequirementsStayVisible(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pay.feature")
	body := "# gi-spec: pay\n# gi-open: clarify FR-2 show the amount still due\nFeature: Pay\n\nScenario: ok\n  * assert true\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out := runCLI(t, "test", path)
	if code != 0 || !strings.Contains(out, "open:\n  clarify: FR-2 show the amount still due\n") {
		t.Fatalf("exit %d\n%s", code, out)
	}
	fail := filepath.Join(dir, "fail.feature")
	body = "# gi-spec: fail\nFeature: Fail\n\nScenario: charge is refused\n  * assert false\n"
	if err := os.WriteFile(fail, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out = runCLI(t, "test", fail)
	if code != 1 || !strings.Contains(out, "open:\n  failed: charge is refused\n") {
		t.Fatalf("exit %d\n%s", code, out)
	}
	plain := filepath.Join(dir, "plain.feature")
	if err := os.WriteFile(plain, []byte("Feature: Plain\n\nScenario: ok\n  * assert true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out = runCLI(t, "test", plain)
	if code != 0 || strings.Contains(out, "open:") {
		t.Fatalf("exit %d\n%s", code, out)
	}
}

func TestRequirementEvidenceStaysOpen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pay.feature")
	body := "# gi-req: AC-9 Payment audit event\n" +
		"Feature: Pay\n\n" +
		"@AC-1\n" +
		"Scenario: paid\n" +
		"  * assert true\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out := runCLI(t, "test", path)
	if code != 0 || !strings.Contains(out, "open:\n  uncovered: AC-9 Payment audit event\n") || strings.Contains(out, "passing:") {
		t.Fatalf("exit %d\n%s", code, out)
	}
	raw, err := os.ReadFile(filepath.Join(dir, ".gi-evidence.json"))
	if err != nil || !strings.Contains(string(raw), `"status": "passing"`) {
		t.Fatalf("evidence file: %v\n%s", err, raw)
	}
	fail := filepath.Join(dir, "dup.feature")
	body = "Feature: Dup\n\n@AC-2\nScenario: duplicate payment\n  * assert false\n"
	if err := os.WriteFile(fail, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out = runCLI(t, "test", fail)
	if code != 1 || !strings.Contains(out, "open:\n  failing: AC-2 duplicate payment\n") {
		t.Fatalf("exit %d\n%s", code, out)
	}
}
