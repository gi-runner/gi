package gi_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gi-runner/gi"
)

func TestRegisteredHelper(t *testing.T) {
	if err := gi.Register("double", func(args []any) (any, error) {
		n, _ := args[0].(int64)
		return n * 2, nil
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { gi.Unregister("double") })
	if err := gi.Register("map", func([]any) (any, error) { return nil, os.ErrInvalid }); err == nil {
		t.Fatal("built-in name was registered")
	}
	path := filepath.Join(t.TempDir(), "helper.feature")
	if err := os.WriteFile(path, []byte("Feature: helper\nScenario: double\n* def n = gi.double(21)\n* match n == 42\n"), 0600); err != nil {
		t.Fatal(err)
	}
	results, err := gi.Run(t.Context(), []string{path}, gi.Options{})
	if err != nil || !results[0].Passed() {
		t.Fatalf("%+v %v", results, err)
	}
}

func TestPublicRunner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "public.feature")
	if err := os.WriteFile(path, []byte("Feature: public\nScenario: values\n* match supplied == 42\n"), 0600); err != nil {
		t.Fatal(err)
	}
	results, err := gi.Run(t.Context(), []string{path}, gi.Options{Vars: map[string]any{"supplied": 42}})
	if err != nil || len(results) != 1 || !results[0].Passed() {
		t.Fatalf("%+v %v", results, err)
	}
}

func TestParallelOutput(t *testing.T) {
	dir := t.TempDir()
	paths := make([]string, 0, 2)
	for _, name := range []string{"one.feature", "two.feature"} {
		path := filepath.Join(dir, name)
		body := "Feature: output\nScenario: one\n* print 'one'\nScenario: two\n* print 'two'\n"
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
	}
	var output bytes.Buffer
	result, err := gi.Run(t.Context(), paths, gi.Options{Jobs: 2, Output: &output})
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 2 || strings.Count(output.String(), "one") != 2 || strings.Count(output.String(), "two") != 2 {
		t.Fatalf("parallel output: %q (%+v)", output.String(), result)
	}
}
