package engine_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gi-runner/gi/internal/engine"
	"github.com/gi-runner/gi/internal/feature"
)

func TestDynamicExamples(t *testing.T) {
	f, err := feature.Parse("rows.feature", `
Feature: rows
  @setup
  Scenario: seed
    * def data = [{ name: 'Ada' }, { name: 'Grace' }]
  Scenario Outline: hello <name>
    * match '<name>' == '<name>'
    Examples:
      | gi.setup().data |
      | x |
`)
	if err != nil {
		t.Fatal(err)
	}
	var outlines int
	for _, sc := range f.Scenarios {
		if sc.Outline != "" {
			outlines++
		}
	}
	if outlines != 1 {
		t.Fatalf("outline stayed dynamic: %d", outlines)
	}
	res, err := engine.RunFile(context.Background(), f, engine.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Passed() || len(res.Scenarios) != 2 {
		t.Fatalf("passed=%v scenarios=%d err=%s", res.Passed(), len(res.Scenarios), scenarioErr(res))
	}
}

func TestCallTree(t *testing.T) {
	dir := t.TempDir()
	inner := filepath.Join(dir, "inner.feature")
	outer := filepath.Join(dir, "outer.feature")
	if err := os.WriteFile(inner, []byte("Feature: inner\nScenario: ping\n* def n = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outer, []byte("Feature: outer\nScenario: go\n* call read('inner.feature')\n* match n == 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := feature.ParseFile(outer)
	if err != nil {
		t.Fatal(err)
	}
	res, err := engine.RunFile(context.Background(), f, engine.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Passed() {
		t.Fatal(scenarioErr(res))
	}
	var found bool
	for _, st := range res.Scenarios[0].Steps {
		if len(st.Calls) == 0 {
			continue
		}
		found = true
		if st.Calls[0].Text != "def n = 1" {
			t.Fatalf("%#v", st.Calls[0])
		}
	}
	if !found {
		t.Fatal("call did not record child steps")
	}
}

func scenarioErr(res engine.FileResult) string {
	for _, sc := range res.Scenarios {
		if sc.Error != "" {
			return sc.Name + ": " + sc.Error
		}
	}
	return ""
}
