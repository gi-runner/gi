package mock

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

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

func TestCancelledRequestDoesNotWaitForActiveScenario(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f, err := feature.Parse("cancel.feature", `Feature: cancel
Scenario: held
* karate.pause(500)
* def response = 'done'
`)
		if err != nil {
			t.Fatal(err)
		}
		handler := Handler(f)
		first := httptest.NewRecorder()
		finished := make(chan struct{})
		go func() {
			defer close(finished)
			handler.ServeHTTP(first, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
		}()
		synctest.Wait()
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		started := time.Now()
		second := httptest.NewRecorder()
		handler.ServeHTTP(second, httptest.NewRequestWithContext(ctx, http.MethodGet, "/", nil))
		if elapsed := time.Since(started); elapsed != 0 {
			t.Fatalf("cancelled request waited %s for the runtime", elapsed)
		}
		<-finished
		if first.Code != http.StatusOK || first.Body.String() != "done" {
			t.Fatalf("queued cancellation disturbed the active request: %d %s", first.Code, first.Body.String())
		}
	})
}

func TestConcurrentRequestsKeepPersistentStateSerialized(t *testing.T) {
	f, err := feature.Parse("counter.feature", `Feature: counter
Background:
* def count = 0
Scenario: increment
* count++
* def response = {count: '#(count)'}
`)
	if err != nil {
		t.Fatal(err)
	}
	handler := Handler(f)
	const count = 100
	values := make(chan int, count)
	var wg sync.WaitGroup
	for range count {
		wg.Go(func() {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
			var body struct {
				Count int `json:"count"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || response.Code != http.StatusOK {
				t.Errorf("response: %d %s %v", response.Code, response.Body.String(), err)
				return
			}
			values <- body.Count
		})
	}
	wg.Wait()
	close(values)
	seen := make(map[int]bool, count)
	for value := range values {
		if value < 1 || value > count || seen[value] {
			t.Errorf("invalid or duplicate counter: %d", value)
		}
		seen[value] = true
	}
	if len(seen) != count {
		t.Fatalf("received %d unique counters, want %d", len(seen), count)
	}
}
