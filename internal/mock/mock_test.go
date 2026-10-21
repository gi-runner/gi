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

func TestStepValidatorExceptionDoesNotSelectFallback(t *testing.T) {
	for _, predicate := range []string{"missingFunction()", "false"} {
		f, err := feature.Parse("mock.feature", `Feature: mock
Scenario: primary
* match request == '#? `+predicate+`'
* def response = 'primary'
Scenario: fallback
* def response = 'fallback'
`)
		if err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		Handler(f).ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x", strings.NewReader("{}")))
		want := 500
		if predicate == "false" {
			want = 200
		}
		if w.Code != want {
			t.Fatalf("%s: %d %s", predicate, w.Code, w.Body.String())
		}
	}
}
