// Package testutil contains shared helpers for repository tests.
package testutil

import (
	"os"
	"strings"
	"testing"
)

// ReadMarkdown normalizes Windows checkouts before tests inspect Markdown syntax.
func ReadMarkdown(tb testing.TB, path string) string {
	tb.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		tb.Fatal(err)
	}
	return strings.ReplaceAll(string(raw), "\r\n", "\n")
}
