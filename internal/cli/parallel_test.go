package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gi-runner/gi/internal/engine"
)

func TestRunFilesKeepsInputOrder(t *testing.T) {
	dir := t.TempDir()
	one := filepath.Join(dir, "one.feature")
	two := filepath.Join(dir, "two.feature")
	if err := os.WriteFile(one, []byte("Feature: one\nScenario: a\n* assert true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(two, []byte("Feature: two\nScenario: b\n* assert true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := runFiles(context.Background(), []string{one, two}, engine.Options{}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "one" || got[1].Name != "two" {
		t.Fatalf("%q %q", got[0].Name, got[1].Name)
	}
	if !got[0].Passed() || !got[1].Passed() {
		t.Fatal("expected both features to pass")
	}
}
