package engine_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gi-runner/gi/internal/engine"
	"github.com/gi-runner/gi/internal/feature"
	"github.com/gi-runner/gi/internal/report"
)

func TestSignalReachesListen(t *testing.T) {
	res, err := engine.RunFile(t.Context(), parse(t, `
Feature: events
Scenario: wait
* def got = karate.listen(2000)
* match got == { id: 7 }
Scenario: send
* karate.signal({ id: 7 })
`), engine.Options{Jobs: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Passed() {
		t.Fatalf("%s / %s", res.Scenarios[0].Error, res.Scenarios[1].Error)
	}
	res, err = engine.RunFile(t.Context(), parse(t, `
Feature: queued
Scenario: already signaled
* karate.signal('early')
* def got = karate.listen(200)
* match got == 'early'
`), engine.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Passed() {
		t.Fatal(res.Scenarios[0].Error)
	}
}

func TestListenTimesOut(t *testing.T) {
	res, err := engine.RunFile(t.Context(), parse(t, `
Feature: timeout
Scenario: nothing arrives
* def got = karate.listen(40)
`), engine.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Passed() || !strings.Contains(res.Scenarios[0].Error, "listen timed out") {
		t.Fatalf("passed=%v err=%s", res.Passed(), res.Scenarios[0].Error)
	}
}

func TestCachedCallKeepsClosedValues(t *testing.T) {
	dir := t.TempDir()
	auth := "Feature: auth\nScenario: token\n* def token = 'secret'\n* def make = function(){ let extra = '-ok'; return function(n){ return token + extra + n } }\n* def sign = make()\n"
	main := "Feature: use\nScenario: first\n* def session = callonce read('auth.feature')\n* match session.sign('!') == 'secret-ok!'\nScenario: second\n* def session = callonce read('auth.feature')\n* match session.sign('!') == 'secret-ok!'\n"
	if err := os.WriteFile(filepath.Join(dir, "auth.feature"), []byte(auth), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "main.feature")
	if err := os.WriteFile(path, []byte(main), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := feature.ParseFile(path)
	if err != nil {
		t.Fatal(err)
	}
	res, err := engine.RunFile(t.Context(), f, engine.Options{Jobs: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Passed() {
		t.Fatalf("%s / %s", res.Scenarios[0].Error, res.Scenarios[1].Error)
	}
}

func TestEmbedAndScreenshotAttach(t *testing.T) {
	page := &shotPage{}
	res, err := engine.RunFile(t.Context(), parse(t, `
Feature: evidence
Scenario: attach
* driver pageUrl
* def shot = screenshot()
* karate.embed('callback arrived', 'text/plain', 'trace')
`), engine.Options{
		Vars:    map[string]any{"pageUrl": "http://page.test"},
		NewPage: func(context.Context) (engine.Page, error) { return page, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Passed() {
		t.Fatal(res.Scenarios[0].Error)
	}
	var png, text bool
	for _, step := range res.Scenarios[0].Steps {
		for _, item := range step.Embeds {
			if item.Mime == "image/png" && string(item.Data) == "png" {
				png = true
			}
			if item.Name == "trace" && string(item.Data) == "callback arrived" {
				text = true
			}
		}
	}
	if !png || !text {
		t.Fatalf("png=%v text=%v steps=%#v", png, text, res.Scenarios[0].Steps)
	}
	dir := t.TempDir()
	doc := report.Document{Title: "evidence", Started: time.Now(), Features: []engine.FileResult{res}}
	htmlPath := filepath.Join(dir, "r.html")
	jsonPath := filepath.Join(dir, "c.json")
	if err := report.WriteHTML(htmlPath, doc); err != nil {
		t.Fatal(err)
	}
	if err := report.WriteCucumber(jsonPath, doc); err != nil {
		t.Fatal(err)
	}
	html, err := os.ReadFile(htmlPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(html), "data:image/png;base64,") || !strings.Contains(string(html), "callback arrived") {
		t.Fatalf("html missing attachment: %s", html)
	}
	raw, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"embeddings"`) || !strings.Contains(string(raw), `"mime_type": "image/png"`) {
		t.Fatalf("cucumber missing attachment: %s", raw)
	}
}

type shotPage struct{ fakePage }

func (p *shotPage) Action(context.Context, string, []any) (any, error) {
	return []byte("png"), nil
}
