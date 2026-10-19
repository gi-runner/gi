package engine_test

import (
	"bytes"
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
	if err := os.WriteFile(child, []byte("Feature: child\nScenario: child\n* eval Object.prototype.polluted = true\n* def sign = function(x){ return x + 1 }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	runRelease(t, filepath.Join(dir, "parent.feature"), `Feature: parent
Scenario: isolation
* def result = call read('child.feature')
* def leaked = ({}).polluted
* match leaked != true
* def n = result.sign(2)
* match n == 3
`, engine.Options{})
}
