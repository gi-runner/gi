package engine_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gi-runner/gi/internal/engine"
	"github.com/gi-runner/gi/internal/feature"
	"github.com/gi-runner/gi/internal/httpx"
)

func TestOAuthClientCredentials(t *testing.T) {
	var hits atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		b, _ := io.ReadAll(r.Body)
		body := string(b)
		for _, want := range []string{"grant_type=client_credentials", "client_id=id", "client_secret=secret", "scope=read"} {
			if !strings.Contains(body, want) {
				http.Error(w, body, http.StatusBadRequest)
				return
			}
		}
		if r.Header.Get("Accept") != "application/json" {
			http.Error(w, "accept", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"tok","token_type":"bearer"}`))
	})
	mux.HandleFunc("GET /secure", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]string{"authorization": r.Header.Get("Authorization")})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	src := `
Feature:

Scenario: token is fetched once
* configure auth = { type: 'oauth2', accessTokenUrl: '/token', clientId: 'id', clientSecret: 'secret', scope: 'read' }
* url baseUrl
* path 'secure'
* method get
* match response.authorization == 'Bearer tok'
* path 'secure'
* method get
* match response.authorization == 'Bearer tok'
`
	f, err := feature.Parse("oauth.feature", src)
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
	if len(res.Scenarios) != 1 || !res.Scenarios[0].Passed {
		t.Fatal(res.Scenarios[0].Error)
	}
	if hits.Load() != 1 {
		t.Fatalf("token requests = %d, want 1", hits.Load())
	}
}

func TestOAuthTokenIsSharedAcrossScenarios(t *testing.T) {
	var hits atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		writeJSON(w, map[string]string{"access_token": "tok"})
	})
	mux.HandleFunc("GET /secure", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]string{"authorization": r.Header.Get("Authorization")})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	src := `
Feature:
Scenario: first
* configure auth = { type: 'oauth2', accessTokenUrl: '/token', clientId: 'id', clientSecret: 'secret' }
* url baseUrl
* path 'secure'
* method get
* match response.authorization == 'Bearer tok'
Scenario: second
* configure auth = { type: 'oauth2', accessTokenUrl: '/token', clientId: 'id', clientSecret: 'secret' }
* url baseUrl
* path 'secure'
* method get
* match response.authorization == 'Bearer tok'
`
	res := runOAuth(t, srv, src)
	if len(res.Scenarios) != 2 || !res.Scenarios[0].Passed || !res.Scenarios[1].Passed {
		t.Fatalf("%+v", res.Scenarios)
	}
	if hits.Load() != 1 {
		t.Fatalf("token requests = %d, want 1", hits.Load())
	}
}

func TestOAuthPasswordGrantRefreshes(t *testing.T) {
	var grants []string
	mux := http.NewServeMux()
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body := string(b)
		grant := "client_credentials"
		switch {
		case strings.Contains(body, "grant_type=password"):
			grant = "password"
			if !strings.Contains(body, "username=ada") || !strings.Contains(body, "password=s3cret") {
				http.Error(w, body, http.StatusBadRequest)
				return
			}
		case strings.Contains(body, "grant_type=refresh_token"):
			grant = "refresh_token"
			if !strings.Contains(body, "refresh_token=rt1") {
				http.Error(w, body, http.StatusBadRequest)
				return
			}
		}
		grants = append(grants, grant)
		token := "tok1"
		extra := `,"expires_in":1,"refresh_token":"rt1"`
		if grant == "refresh_token" {
			token = "tok2"
			extra = `,"expires_in":60,"refresh_token":"rt2"`
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"` + token + `"` + extra + `}`))
	})
	mux.HandleFunc("GET /secure", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]string{"authorization": r.Header.Get("Authorization")})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	src := `
Feature:
Scenario: refresh after the access token expires
* configure auth = { type: 'oauth2', grantType: 'password', accessTokenUrl: '/token', clientId: 'id', clientSecret: 'secret', username: 'ada', password: 's3cret' }
* url baseUrl
* path 'secure'
* method get
* match response.authorization == 'Bearer tok1'
* karate.pause(1500)
* path 'secure'
* method get
* match response.authorization == 'Bearer tok2'
`
	res := runOAuth(t, srv, src)
	if len(res.Scenarios) != 1 || !res.Scenarios[0].Passed {
		t.Fatal(res.Scenarios[0].Error)
	}
	if strings.Join(grants, ",") != "password,refresh_token" {
		t.Fatalf("grants = %v", grants)
	}
}

func TestOAuthGrantRejectsAPartialPassword(t *testing.T) {
	src := `
Feature:
Scenario: missing user
* configure auth = { type: 'oauth2', grantType: 'password', accessTokenUrl: '/token', clientId: 'id', clientSecret: 'secret' }
* assert false
`
	f, err := feature.Parse("oauth.feature", src)
	if err != nil {
		t.Fatal(err)
	}
	res, err := engine.RunFile(context.Background(), f, engine.Options{HTTP: httpx.New(http.DefaultClient)})
	if err != nil {
		t.Fatal(err)
	}
	if res.Scenarios[0].Passed || !strings.Contains(res.Scenarios[0].Error, "username and password") {
		t.Fatalf("%+v", res.Scenarios[0])
	}
}

func runOAuth(t *testing.T, srv *httptest.Server, src string) engine.FileResult {
	t.Helper()
	f, err := feature.Parse("oauth.feature", src)
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
	return res
}
