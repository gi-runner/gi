package engine_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gi-runner/gi/internal/engine"
	"github.com/gi-runner/gi/internal/httpx"
)

func TestOptionalEmbedRemovesTheKey(t *testing.T) {
	runRelease(t, "optional.feature", `Feature: optional
Scenario: null removes
* def data = { a: 'hello', b: null, c: null }
* def json = { foo: '#(data.a)', bar: '#(data.b)', baz: '##(data.c)' }
* match json == { foo: 'hello', bar: null }
Scenario: undefined removes
* def exists = function(v){ return v ? 'present' : null }
* def name = 'foo'
* def json = { name: '##(exists(name))', country: '##(exists(country))' }
* match json == { name: 'present' }
Scenario: value stays
* def name = 'Bob'
* def json = { name: '##(name)' }
* match json == { name: 'Bob' }
`, engine.Options{})
}

func TestCallResultCarriesTheResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"token":"t1"}`))
	}))
	defer server.Close()
	dir := t.TempDir()
	login := "Feature: login\nScenario: login\n* url baseUrl\n* method post\n"
	if err := os.WriteFile(filepath.Join(dir, "login.feature"), []byte(login), 0600); err != nil {
		t.Fatal(err)
	}
	runRelease(t, filepath.Join(dir, "main.feature"), `Feature: main
Scenario: call
* def result = call read('login.feature')
* match result.response == { token: 't1' }
* match result.responseStatus == 201
* match result.responseHeaders['Content-Type'][0] == 'application/json'
`, engine.Options{HTTP: httpx.New(server.Client()), Vars: map[string]any{"baseUrl": server.URL}})
}

func TestXPathWithoutSpace(t *testing.T) {
	runRelease(t, "xpath.feature", `Feature: xpath
Scenario: joined
* def doc = <root><v>1</v><q><n>a</n></q></root>
* match doc/root/v == '1'
* match doc/root/q == <q><n>a</n></q>
* def ten = 10
* def half = ten/2
* match half == 5
`, engine.Options{})
}
