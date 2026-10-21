package cli

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
