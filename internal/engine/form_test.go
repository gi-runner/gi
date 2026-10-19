package engine_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gi-runner/gi/internal/engine"
	"github.com/gi-runner/gi/internal/feature"
	"github.com/gi-runner/gi/internal/httpx"
)

func TestFormAndAuth(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /form", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		writeJSON(w, map[string]string{"type": r.Header.Get("Content-Type"), "body": string(b)})
	})
	mux.HandleFunc("GET /auth", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]string{"authorization": r.Header.Get("Authorization")})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	src := `
Feature:

Scenario: form body
* url baseUrl
* path 'form'
* form field name = 'Ada'
* form fields { city: 'London' }
* method post
* match response.body == 'city=London&name=Ada'
* match response.type == 'application/x-www-form-urlencoded'

Scenario: basic auth
* configure auth = { type: 'basic', username: 'ada', password: 'secret' }
* url baseUrl
* path 'auth'
* method get
* match response.authorization == 'Basic YWRhOnNlY3JldA=='

Scenario: bearer yields to a header
* configure auth = { type: 'bearer', token: 'abc' }
* header Authorization = 'Bearer other'
* url baseUrl
* path 'auth'
* method get
* match response.authorization == 'Bearer other'
`
	f, err := feature.Parse("form.feature", src)
	if err != nil {
		t.Fatal(err)
	}
	res, err := engine.RunFile(context.Background(), f, engine.Options{
		HTTP: httpx.New(srv.Client()),
		Vars: map[string]any{"baseUrl": srv.URL},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, sc := range res.Scenarios {
		if !sc.Passed {
			t.Errorf("%s\n%s", sc.Name, sc.Error)
		}
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	raw, err := json.Marshal(v)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, _ = w.Write(raw)
}
