package mock

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gi-runner/gi/internal/feature"
)

func TestPersistentBackgroundAndRouteVariables(t *testing.T) {
	f, err := feature.Parse("mock.feature", `Feature: mock
Background:
* def count = 0
Scenario: pathMatches('/users/{id}') && methodIs('get') && paramValue('q') == 'yes'
* count++
* def response = { id: '#(pathParams.id)', count: '#(count)' }
`)
	if err != nil {
		t.Fatal(err)
	}
	h := Handler(f)
	for _, want := range []string{`"count":1`, `"count":2`} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/users/42?q=yes", nil))
		if w.Code != 200 || !strings.Contains(w.Body.String(), want) || !strings.Contains(w.Body.String(), `"id":"42"`) {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
	}
}

func TestMatcherErrorsFailClosed(t *testing.T) {
	f, err := feature.Parse("mock.feature", "Feature: mock\nScenario: typo('/x')\n* def response = 'wrong'\n")
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	Handler(f).ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x", nil))
	if w.Code != 500 {
		t.Fatalf("matcher error returned %d: %s", w.Code, w.Body.String())
	}
}
