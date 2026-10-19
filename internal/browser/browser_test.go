package browser

import (
	"context"
	"github.com/gi-runner/gi/internal/engine"
	"github.com/gi-runner/gi/internal/feature"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestChromePage(t *testing.T) {
	if !chromePresent() {
		t.Skip("chrome is not installed")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<!doctype html><input id="name"><button id="go" onclick="document.getElementById('out').textContent=document.getElementById('name').value"></button><div id="out"></div>`))
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	page, err := Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = page.Close() }()
	if err := page.Open(ctx, srv.URL); err != nil {
		t.Fatal(err)
	}
	if err := page.Fill(ctx, "#name", "Ada"); err != nil {
		t.Fatal(err)
	}
	if err := page.Click(ctx, "#go"); err != nil {
		t.Fatal(err)
	}
	got, err := page.Text(ctx, "#out")
	if err != nil {
		t.Fatal(err)
	}
	if got != "Ada" {
		t.Fatalf("%q", got)
	}
}

func chromePresent() bool {
	if runtime.GOOS == "darwin" {
		if _, err := exec.LookPath("/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"); err == nil {
			return true
		}
	}
	for _, name := range []string{"google-chrome", "chromium", "chrome"} {
		if _, err := exec.LookPath(name); err == nil {
			return true
		}
	}
	return false
}

func TestDriverFeatureAndSharedBrowser(t *testing.T) {
	if !chromePresent() {
		t.Skip("chrome is not installed")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<!doctype html><title>Native Driver</title><input id="name"><button id="go" onclick="document.getElementById('out').textContent=document.getElementById('name').value">Go</button><div id="out"></div><select id="choice"><option value="a">A</option><option value="b">B</option></select>`))
	}))
	defer server.Close()
	dir := t.TempDir()
	path := filepath.Join(dir, "main.feature")
	helper := `Feature: shared browser
Scenario: enter name
* waitFor('#name').clear().input('Grace')
* click('{button}Go')
`
	if err := os.WriteFile(filepath.Join(dir, "helper.feature"), []byte(helper), 0600); err != nil {
		t.Fatal(err)
	}
	f, err := feature.Parse(path, `Feature: Driver API
Scenario: browser
* driver baseUrl
* match driver.title == 'Native Driver'
* match driver.url contains baseUrl
* assert exists('#name')
* assert !exists('#missing')
* call 'helper.feature'
* waitForText('#out','Grace')
* match text('#out') == 'Grace'
* match locate('#out').text == 'Grace'
* select('#choice','b')
* match value('#choice') == 'b'
* match attribute('#go','id') == 'go'
* assert locateAll('button').length == 1
* def image = screenshot()
* assert image.length > 100
* assert image[0] == 137
* match script('document.title') == 'Native Driver'
`)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	result, err := engine.RunFile(ctx, f, engine.Options{NewPage: Open, Vars: map[string]any{"baseUrl": server.URL}})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed() {
		for _, sc := range result.Scenarios {
			t.Error(sc.Error)
		}
	}
}
