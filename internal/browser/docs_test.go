package browser

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gi-runner/gi/internal/engine"
	"github.com/gi-runner/gi/internal/feature"
)

func TestDocumentedBrowserExample(t *testing.T) {
	if !chromePresent() {
		t.Skip("chrome is not installed")
	}
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "browser.md"))
	if err != nil {
		t.Fatal(err)
	}
	_, block, ok := strings.Cut(string(raw), "```gherkin\n")
	if !ok {
		t.Fatal("browser documentation has no Gherkin example")
	}
	source, _, ok := strings.Cut(block, "```")
	if !ok {
		t.Fatal("unclosed browser example")
	}
	f, err := feature.Parse(filepath.Join(t.TempDir(), "greeting.feature"), source)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<!doctype html><input id="name"><button id="go" onclick="document.getElementById('out').textContent=document.getElementById('name').value">Greet</button><div id="out"></div>`))
	}))
	defer server.Close()
	result, err := engine.RunFile(t.Context(), f, engine.Options{NewPage: Open, Vars: map[string]any{"pageUrl": server.URL}})
	if err != nil || !result.Passed() {
		t.Fatalf("documented browser example failed: %v %+v", err, result.Scenarios)
	}
}
