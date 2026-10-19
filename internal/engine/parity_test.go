package engine_test

import (
	"context"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gi-runner/gi/internal/engine"
	"github.com/gi-runner/gi/internal/feature"
	"github.com/gi-runner/gi/internal/httpx"
	"github.com/gi-runner/gi/internal/suite"
)

func TestScopedCallsPreserveConfigurationAndClosures(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"karate-config.js": `function(){ karate.configure('headers',function(){return {'X-Token':token}}); return {token:'original',fromConfig:1,closure:(function(){var count=0;return function(){return ++count}})()} }`,
		"shared.feature": `Feature: shared
Scenario: change
* def token = 'shared'
* configure headers = (function(){var suffix='-closure';return function(){return {'X-Token':token+suffix}}})()
* configure afterScenario = function(){karate.set('closed',true)}
* def count = closure()
`,
		"isolated.feature": `Feature: isolated
Scenario: change
* def token = 'isolated'
* configure headers = { 'X-Token':'isolated' }
* def count = closure()
`,
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(dir, "main.feature")
	sender := releaseSenderFunc(func(_ context.Context, call engine.Call) (engine.Reply, error) {
		if got := http.Header(call.Header).Get("X-Token"); got != "shared-closure" {
			t.Errorf("header callback lost closure: %s", got)
		}
		return engine.Reply{Status: 200}, nil
	})
	f, err := feature.Parse(path, `Feature: scopes
Scenario: scopes
* def fromConfig = 2
* call 'shared.feature'
* match count == 1
* def isolated = call 'isolated.feature'
* match isolated.count == 2
* match token == 'shared'
* match fromConfig == 2
* url 'http://scope.example.test'
* method get
`)
	if err != nil {
		t.Fatal(err)
	}
	res, err := engine.RunFile(t.Context(), f, engine.Options{HTTP: sender, Suite: suite.Open(path, "")})
	if err != nil || !res.Passed() {
		t.Fatalf("%s (%v)", scenarioErr(res), err)
	}
	if res.Scenarios[0].Vars["closed"] != true {
		t.Fatal("shared afterScenario hook was not propagated")
	}
}

func TestNativeXMLAndJSONPaths(t *testing.T) {
	runRelease(t, "paths.feature", `Feature: paths
Scenario: XML
* def doc = <r xmlns:n="urn:items"><n:item id="1">one</n:item><n:item id="2">two</n:item></r>
* match doc /r/n:item[@id='2'] == 'two'
* match doc count(/r/n:item) == 2
* set doc /r/n:item[@id='1'] = 'changed'
* set doc /r/n:item[@id='2']/@id = '3'
* remove doc /r/n:item[@id='3']
* match karate.xmlPath(doc, '/r/n:item') == 'changed'
* xmlstring encoded = doc
* match encoded contains 'xmlns:n="urn:items"'
* match doc == <r xmlns:x="urn:items"><x:item id="1">changed</x:item></r>
Scenario: JSONPath
* def data = {users:[{name:'Ada',age:36},{name:'Grace',age:20}]}
* def adults = get data $.users[?(@.age>30)].name
* match adults == ['Ada']
* match karate.jsonPath(data,'$..name') == ['Ada','Grace']
* match data.users[0:1].name == ['Ada']
`, engine.Options{})
}

func TestXMLHTTPPreservesContent(t *testing.T) {
	const body = `<p xmlns:x="urn:x">before<x:b>bold</x:b>after</p>`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/xml" {
			t.Errorf("content type: %s", r.Header.Get("Content-Type"))
		}
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()
	runRelease(t, "xml-http.feature", `Feature: XML HTTP
Scenario: round trip
* url baseUrl
* request <request><name>Ada</name></request>
* method post
* match response /p/x:b == 'bold'
* xmlstring text = response
* match text contains 'before<x:b>bold</x:b>after'
`, engine.Options{HTTP: httpx.New(server.Client()), Vars: map[string]any{"baseUrl": server.URL}})
}

func TestLifecycleAndSpecialTags(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hooks.feature")
	config := `function(){
      karate.configure('afterScenarioOutline',function(){karate.set('outlineDone',true)})
      karate.configure('afterFeature',function(){karate.set('featureDone',true)})
      return {}
    }`
	if err := os.WriteFile(filepath.Join(dir, "karate-config.js"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	f, err := feature.Parse(path, `Feature: hooks
Scenario Outline: rows
* match number == '#string'
Examples:
| number |
| 1 |
| 2 |
@fail
Scenario: expected failure
* assert false
@fail
Scenario: unexpected success
* assert true
@report=false
Scenario: suppressed
* def secret = 'must-not-be-in-artifacts'
* assert false
Scenario: failure hook
* configure onStepFailure = function(info){karate.set('failureLine',info.step.line);info.proceed()}
* assert false
* def continued = true
`)
	if err != nil {
		t.Fatal(err)
	}
	res, err := engine.RunFile(t.Context(), f, engine.Options{Jobs: 1, Suite: suite.Open(path, "")})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Scenarios) != 6 {
		t.Fatalf("scenarios: %d", len(res.Scenarios))
	}
	if res.Scenarios[1].Vars["outlineDone"] != true {
		t.Fatal("outline hook did not run")
	}
	if !res.Scenarios[2].Passed || res.Scenarios[3].Passed {
		t.Fatal("@fail did not invert outcomes")
	}
	hidden := res.Scenarios[4]
	if !hidden.ReportDisabled || len(hidden.Steps) != 0 || strings.Contains(hidden.Error, "must-not") {
		t.Fatalf("report details leaked: %+v", hidden)
	}
	last := res.Scenarios[5]
	if last.Passed || last.Vars["continued"] != true || last.Vars["featureDone"] != true {
		t.Fatalf("failure/feature hooks: %+v", last)
	}
}

func TestCachedSharedCookiesAndCallbackReplay(t *testing.T) {
	var logins atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" {
			logins.Add(1)
			http.SetCookie(w, &http.Cookie{Name: "sid", Value: "session", Path: "/"})
			return
		}
		if c, err := r.Cookie("sid"); err != nil || c.Value != "session" {
			t.Error("cached shared call lost cookie")
		}
		if r.Header.Get("X-Token") != "cached" {
			t.Error("cached shared call lost header callback")
		}
	}))
	defer srv.Close()
	dir := t.TempDir()
	path := filepath.Join(dir, "main.feature")
	child := `Feature: login
Scenario: login
* path 'login'
* method get
* def token = 'cached'
* configure headers = function(){return {'X-Token':token}}
`
	if err := os.WriteFile(filepath.Join(dir, "login.feature"), []byte(child), 0600); err != nil {
		t.Fatal(err)
	}
	runRelease(t, path, `Feature: cached state
Background:
* url baseUrl
Scenario: first
* callonce 'login.feature'
* path 'check'
* method get
Scenario: second
* callonce 'login.feature'
* path 'check'
* method get
`, engine.Options{HTTP: httpx.New(srv.Client()), Vars: map[string]any{"baseUrl": srv.URL}})
	if logins.Load() != 1 {
		t.Fatalf("logins: %d", logins.Load())
	}
}

func TestHTTPConfigurationTLSAndProxy(t *testing.T) {
	tlsServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"ok":true}`)) }))
	defer tlsServer.Close()
	dir := t.TempDir()
	caPath := filepath.Join(dir, "ca.pem")
	certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: tlsServer.Certificate().Raw})
	if err := os.WriteFile(caPath, certificate, 0600); err != nil {
		t.Fatal(err)
	}
	client := httpx.New(nil)
	defer client.CloseIdleConnections()
	runRelease(t, filepath.Join(dir, "tls.feature"), `Feature: TLS config
Scenario: explicit trust all
* configure ssl = true
* configure connectTimeout = 1000
* url baseUrl
* method get
* match response.ok == true
Scenario: configured CA
* configure ssl = {trustAll:false, caCert:'ca.pem'}
* url baseUrl
* method get
* match response.ok == true
`, engine.Options{HTTP: client, Vars: map[string]any{"baseUrl": tlsServer.URL}})
	var proxied atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxied.Add(1)
		if r.URL.Host != "target.invalid" {
			t.Errorf("proxy request: %s", r.URL)
		}
		_, _ = w.Write([]byte(`{"proxied":true}`))
	}))
	defer proxy.Close()
	runRelease(t, "proxy.feature", `Feature: proxy
Scenario: proxy
* configure proxy = proxyUrl
* url 'http://target.invalid/path'
* method get
* match response.proxied == true
`, engine.Options{HTTP: client, Vars: map[string]any{"proxyUrl": proxy.URL}})
	if proxied.Load() != 1 {
		t.Fatalf("proxy calls: %d", proxied.Load())
	}
}

func TestSoapAction(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("SoapAction") != "urn:hello" || r.Header.Get("Content-Type") != "text/xml" {
			t.Errorf("SOAP request: %s %v", r.Method, r.Header)
		}
		w.Header().Set("Content-Type", "text/xml")
		_, _ = w.Write([]byte(`<response><message>hello</message></response>`))
	}))
	defer server.Close()
	client := httpx.New(server.Client())
	defer client.CloseIdleConnections()
	runRelease(t, "soap.feature", `Feature: SOAP
Scenario: SOAP
* url baseUrl
* request <request><name>Ada</name></request>
* soap action 'urn:hello'
* match response /response/message == 'hello'
`, engine.Options{HTTP: client, Vars: map[string]any{"baseUrl": server.URL}})
}

func TestXMLCreationCopyAndRootSelection(t *testing.T) {
	runRelease(t, "xml-create.feature", `Feature: XML creation
Scenario: tree
* def doc = <root/>
* set doc /root/deep/name = 'Ada'
* set doc /root/deep/@id = '1'
* match doc /root/deep/name == 'Ada'
* copy other = doc
* set other /root/deep/name = 'Grace'
* match doc /root/deep/name == 'Ada'
* match other /root/deep/name == 'Grace'
* match doc / == <root><deep id="1"><name>Ada</name></deep></root>
`, engine.Options{})
}

func TestFunctionCallsAndParsedArguments(t *testing.T) {
	sender := releaseSenderFunc(func(_ context.Context, call engine.Call) (engine.Reply, error) {
		if call.URL != "http://arguments.example.test/1/2/end" {
			t.Errorf("path arguments: %s", call.URL)
		}
		return engine.Reply{Status: http.StatusOK, Body: []byte(`{"names":["Ada"]}`)}, nil
	})
	runRelease(t, "arguments.feature", `Feature: function and argument syntax
Scenario: grammar
* def reader = function(x){return {number:x}}
* call reader 2
* match number == 2
* def isolated = call reader 3
* match number == 2
* match isolated.number == 3
* url 'http://arguments.example.test'
* path /a,b/.test('a,b') ? [1,2] : [], null, 'end'
* method get
* match $.names[*] == ['Ada']
`, engine.Options{HTTP: sender})
}
