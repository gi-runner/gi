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
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
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
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
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

func TestFramesDialogsWindowsAndIntercept(t *testing.T) {
	if !chromePresent() {
		t.Skip("chrome is not installed")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/other", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<!doctype html><title>Other</title><button id="load" onclick="fetch('/secret').then(r => r.text()).then(t => document.getElementById('out').textContent = t)">load</button><div id="out"></div>`))
	})
	mux.HandleFunc("/secret", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<!doctype html><title>One</title><iframe id="box" srcdoc="<p id='inner'>inside</p>"></iframe><button id="ask" onclick="document.getElementById('out').textContent = confirm('leave?') ? 'yes' : 'no'">ask</button><button id="load" onclick="fetch('/secret').then(r => r.text()).then(t => document.getElementById('out').textContent = t)">load</button><div id="out"></div>`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	f, err := feature.Parse("page.feature", `Feature: page
Scenario: pieces
* driver baseUrl + '/'
* driver.frame('#box')
* match text('#inner') == 'inside'
* driver.frame()
* assert !exists('#inner')
* driver.dialog(false)
* click('#ask')
* match text('#out') == 'no'
* match driver.dialogText() == 'leave?'
* driver.intercept('/secret', { status: 200, body: 'hidden' })
* click('#load')
* waitForText('#out', 'hidden')
* driver.window(otherUrl)
* match driver.title == 'Other'
* click('#load')
* waitForText('#out', 'hidden')
* def tabs = driver.window()
* def back = tabs.find(function(t){ return t.title == 'One' }).index
* driver.window(back)
* match driver.title == 'One'
`)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	res, err := engine.RunFile(ctx, f, engine.Options{
		NewPage: Open,
		Vars:    map[string]any{"baseUrl": server.URL, "otherUrl": server.URL + "/other"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Passed() {
		for _, sc := range res.Scenarios {
			if !sc.Passed {
				t.Error(sc.Error)
			}
		}
	}
}

func TestWindowOperationStopsAtTheStepDeadline(t *testing.T) {
	if !chromePresent() {
		t.Skip("chrome is not installed")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/stall" {
			timer := time.NewTimer(5 * time.Second)
			defer timer.Stop()
			select {
			case <-timer.C:
			case <-r.Context().Done():
			}
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<!doctype html><title>Ready</title>`))
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	opened, err := Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = opened.Close() }()
	p, ok := opened.(*page)
	if !ok {
		t.Fatalf("%T", opened)
	}
	if err := p.Open(ctx, server.URL+"/"); err != nil {
		t.Fatal(err)
	}
	step, stop := context.WithTimeout(ctx, 40*time.Millisecond)
	defer stop()
	started := time.Now()
	err = p.activeTab().run(step, func(c context.Context) error {
		<-c.Done()
		return c.Err()
	})
	if err == nil || time.Since(started) > 500*time.Millisecond {
		t.Fatalf("tab operation: %v after %s", err, time.Since(started))
	}
	stall, stopStall := context.WithTimeout(ctx, 40*time.Millisecond)
	defer stopStall()
	started = time.Now()
	err = p.openWindow(stall, server.URL+"/stall")
	if err == nil || time.Since(started) > 500*time.Millisecond {
		t.Fatalf("window open: %v after %s", err, time.Since(started))
	}
	if len(p.tabs) != 1 {
		t.Fatalf("failed open left %d tabs", len(p.tabs))
	}
	if err := p.openWindow(ctx, server.URL+"/ready"); err != nil {
		t.Fatal(err)
	}
	if err := p.useWindow(ctx, 0); err != nil {
		t.Fatal(err)
	}
	if len(p.tabs) != 2 {
		t.Fatalf("switching attached the target again: %d tabs", len(p.tabs))
	}
}

func TestFeatureFileUpload(t *testing.T) {
	if !chromePresent() {
		t.Skip("chrome is not installed")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<!doctype html><input id="one" type="file"><input id="many" type="file" multiple>`))
	}))
	defer server.Close()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "first.txt"), []byte("first payload"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "second.txt"), []byte("second payload"), 0600); err != nil {
		t.Fatal(err)
	}
	f, err := feature.Parse(filepath.Join(dir, "upload.feature"), `Feature: uploads
Scenario: file inputs
* driver baseUrl
* driver.inputFile('#one','first.txt')
* match script("document.querySelector('#one').files[0].name") == 'first.txt'
* match script("document.querySelector('#one').files[0].size") == 13
* inputFile('//*[@id="many"]',['first.txt','second.txt'])
* match script("Array.from(document.querySelector('#many').files).map(f => f.name)") == ['first.txt','second.txt']
* locate('#one').inputFile('second.txt')
* match script("document.querySelector('#one').files[0].name") == 'second.txt'
* inputFile('#one',[])
* match script("document.querySelector('#one').files.length") == 0
`)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	res, err := engine.RunFile(ctx, f, engine.Options{NewPage: Open, Vars: map[string]any{"baseUrl": server.URL}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Passed() {
		for _, sc := range res.Scenarios {
			if !sc.Passed {
				t.Error(sc.Error)
			}
		}
	}
}

func TestDialogDuringInitialNavigation(t *testing.T) {
	if !chromePresent() {
		t.Skip("chrome is not installed")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<script>alert('initial dialog')</script><div id="done">loaded</div>`))
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	p, err := Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()
	if err := p.Open(ctx, srv.URL); err != nil {
		t.Fatal(err)
	}
}
