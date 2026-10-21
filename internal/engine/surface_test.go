package engine_test

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gi-runner/gi/internal/engine"
	"github.com/gi-runner/gi/internal/feature"
)

func TestRuntimeHelpers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "surface.feature")
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	_, portText, _ := net.SplitHostPort(ln.Addr().String())
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	var log bytes.Buffer
	started := time.Now()
	runRelease(t, path, `Feature: surface
Scenario: info
* def info = karate.info
* match info.scenarioName == 'info'
* match info.featureFileName == 'surface.feature'
* match karate.scenario.name == 'info'
* match karate.feature.fileName == 'surface.feature'
* karate.log('from-log')
* karate.pause(40)
* def written = karate.write('hello', 'note.txt')
* match written contains 'note.txt'
* karate.waitForPort('127.0.0.1', port, 1000, 20)
* def before = true
* karate.abort()
* def after = true
* match before == true
* match after == '#notpresent'
`, engine.Options{Stdout: &log, Vars: map[string]any{"port": port}})
	if !strings.Contains(log.String(), "from-log") {
		t.Fatalf("log = %q", log.String())
	}
	if time.Since(started) < 40*time.Millisecond {
		t.Fatal("pause returned early")
	}
	body, err := os.ReadFile(filepath.Join(dir, "target", "note.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "hello" {
		t.Fatalf("wrote %q", body)
	}
}

func TestWriteRejectsEscape(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "escape.feature")
	f, err := feature.Parse(path, "Feature: escape\nScenario: escape\n* karate.write('x', '../nope.txt')\n")
	if err != nil {
		t.Fatal(err)
	}
	res, err := engine.RunFile(t.Context(), f, engine.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Scenarios[0].Passed {
		t.Fatal("path escape was accepted")
	}
}

func TestXMLObjectAndTable(t *testing.T) {
	runRelease(t, "xml.feature", `Feature: xml
Scenario: object and table
* xml doc = { user: { '@id': '1', name: 'Ada' } }
* match karate.xmlPath(doc, '/user/@id') == '1'
* match karate.xmlPath(doc, '/user/name') == 'Ada'
* xml search =
"""
<search xmlns:acc="urn:acc"><acc:account/></search>
"""
* set search /search/acc:account
| path | value |
| acc:phone | '1234' |
* match karate.xmlPath(search, '/search/acc:account/acc:phone') == '1234'
`, engine.Options{})
}

func TestAssignedCallIsolation(t *testing.T) {
	dir := t.TempDir()
	child := filepath.Join(dir, "child.feature")
	body := `Feature: child
Scenario: child
* eval let secret = 'hidden'
* eval const fixed = 2
* def seen = secret + fixed
* def reveal = function(){ return secret }
* eval Object.prototype.polluted = true
* eval Object.defineProperty(Object.prototype, 'hidden', { value: true })
* eval Array.prototype.push = function(){ return 0 }
* def sign = function(x){ return x + 1 }
`
	if err := os.WriteFile(child, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	runRelease(t, filepath.Join(dir, "parent.feature"), `Feature: parent
Scenario: isolation
* def result = call read('child.feature')
* def kind = typeof secret
* match kind == 'undefined'
* def leaked = ({}).polluted
* match leaked != true
* def hidden = ({}).hidden
* match hidden != true
* def pushed = [1].push(2)
* match pushed == 2
* match result.seen == 'hidden2'
* def revealed = result.reveal()
* match revealed == 'hidden'
* def n = result.sign(2)
* match n == 3
`, engine.Options{})
}

func TestSetupRealmDoesNotLeak(t *testing.T) {
	runRelease(t, "setup-realm.feature", `Feature: setup
@setup
Scenario: prepare
* eval let setupSecret = 'nope'
* eval Object.defineProperty(Array.prototype, 'marked', { value: true })
* def ready = true
Scenario: caller
* def prepared = karate.setup()
* match prepared.ready == true
* def kind = typeof setupSecret
* match kind == 'undefined'
* def marked = [].marked
* match marked != true
`, engine.Options{})
}

func TestInputFileResolutionAndValidation(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(file, []byte("note"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, ref := range []string{"note.txt", "this:note.txt", "classpath:note.txt", "file:" + file} {
		t.Run(ref, func(t *testing.T) {
			page := &uploadPage{}
			runRelease(t, filepath.Join(dir, "upload.feature"), "Feature: upload\nScenario: upload\n* driver 'http://page.test'\n* inputFile('#file', ref)\n", engine.Options{Vars: map[string]any{"ref": ref}, NewPage: func(context.Context) (engine.Page, error) { return page, nil }})
			if len(page.files) != 1 || page.files[0] != file {
				t.Fatalf("resolved files: %v", page.files)
			}
		})
	}
	for _, ref := range []string{"missing.txt", "this:."} {
		t.Run(ref, func(t *testing.T) {
			page := &uploadPage{}
			f, err := feature.Parse(filepath.Join(dir, "invalid.feature"), "Feature: upload\nScenario: invalid\n* driver 'http://page.test'\n* inputFile('#file',ref)\n")
			if err != nil {
				t.Fatal(err)
			}
			res, err := engine.RunFile(t.Context(), f, engine.Options{Vars: map[string]any{"ref": ref}, NewPage: func(context.Context) (engine.Page, error) { return page, nil }})
			if err != nil {
				t.Fatal(err)
			}
			if res.Passed() || page.files != nil {
				t.Fatalf("invalid file reached browser: %#v", res)
			}
		})
	}
}

type uploadPage struct {
	fakePage
	files []string
}

func (p *uploadPage) Action(_ context.Context, name string, args []any) (any, error) {
	if name != "inputFile" {
		return nil, fmt.Errorf("unexpected action %s", name)
	}
	files, ok := args[1].([]string)
	if !ok {
		return nil, fmt.Errorf("unexpected upload files %T", args[1])
	}
	p.files = files
	return true, nil
}
