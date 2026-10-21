package testutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadMarkdownLineEndings(t *testing.T) {
	const source = "## test\n\n```gherkin\nFeature: example\nScenario: run\n* assert true\n```\n"
	for _, tc := range []struct{ name, text string }{
		{"LF", source},
		{"CRLF", strings.ReplaceAll(source, "\n", "\r\n")},
		{"mixed", strings.Replace(source, "## test\n", "## test\r\n", 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "example.md")
			if err := os.WriteFile(path, []byte(tc.text), 0600); err != nil {
				t.Fatal(err)
			}
			if got := ReadMarkdown(t, path); got != source {
				t.Fatalf("Markdown = %q, want %q", got, source)
			}
		})
	}
}
