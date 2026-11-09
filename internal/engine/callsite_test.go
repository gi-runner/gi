package engine_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gi-runner/gi/internal/engine"
	"github.com/gi-runner/gi/internal/feature"
	"github.com/gi-runner/gi/internal/httpx"
)

func TestSamePathKeepsEachCallSite(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	f, err := feature.Parse("orders.feature", `
Feature: orders
Scenario: two gets
* url baseUrl
* path 'orders'
* method get
* path 'orders'
* method get
`)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var names []string
	res, err := engine.RunFile(t.Context(), f, engine.Options{
		HTTP: httpx.New(srv.Client()),
		Vars: map[string]any{"baseUrl": srv.URL},
		OnRequest: func(ex engine.Exchange) {
			mu.Lock()
			names = append(names, ex.Endpoint)
			mu.Unlock()
		},
	})
	if err != nil || !res.Passed() {
		t.Fatalf("run: %v %+v", err, res)
	}
	if len(names) != 2 || names[0] == names[1] {
		t.Fatalf("call sites = %v", names)
	}
	for _, name := range names {
		if !strings.HasPrefix(name, "orders.feature:") || !strings.HasSuffix(name, " GET /orders") {
			t.Fatalf("call site = %s", name)
		}
	}
}
