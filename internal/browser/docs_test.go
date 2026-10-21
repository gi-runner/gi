package browser

import (
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gi-runner/gi/internal/engine"
	"github.com/gi-runner/gi/internal/feature"
	"github.com/gi-runner/gi/internal/mock"
	"github.com/gi-runner/gi/internal/testutil"
)

func TestDocumentedBrowserExample(t *testing.T) {
	if !chromePresent() {
		t.Skip("chrome is not installed")
	}
	text := testutil.ReadMarkdown(t, filepath.Join("..", "..", "docs", "browser.md"))
	_, block, ok := strings.Cut(text, "```gherkin\n")
	if !ok {
		t.Fatal("browser documentation has no Gherkin example")
	}
	source, rest, ok := strings.Cut(block, "```")
	if !ok {
		t.Fatal("unclosed browser example")
	}
	f, err := feature.Parse(filepath.Join(t.TempDir(), "greeting.feature"), source)
	if err != nil {
		t.Fatal(err)
	}
	_, pageBlock, ok := strings.Cut(rest, "```gherkin\n")
	if !ok {
		t.Fatal("browser docs have no example page")
	}
	pageSource, _, ok := strings.Cut(pageBlock, "```")
	if !ok {
		t.Fatal("unclosed browser mock example")
	}
	page, err := feature.Parse(filepath.Join(filepath.Dir(f.Path), "greeting.mock.feature"), pageSource)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(mock.Handler(page))
	defer server.Close()
	result, err := engine.RunFile(t.Context(), f, engine.Options{NewPage: Open, Vars: map[string]any{"pageUrl": server.URL}})
	if err != nil || !result.Passed() {
		t.Fatalf("documented browser example failed: %v %+v", err, result.Scenarios)
	}
}
