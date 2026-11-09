package load

import (
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestLoadReusesOneOAuthToken(t *testing.T) {
	var tokens atomic.Int64
	var calls atomic.Int64
	f := mustFeature(t, `Feature: secure
Scenario: read
* configure auth = { type: 'oauth2', accessTokenUrl: 'http://load.example.test/token', clientId: 'id', clientSecret: 'secret' }
* url 'http://load.example.test'
* path 'secure'
* method get
* status 200
`)
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/token" {
			tokens.Add(1)
			body := io.NopCloser(strings.NewReader(`{"access_token":"tok","token_type":"bearer"}`))
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: body, Request: r}, nil
		}
		calls.Add(1)
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: http.NoBody, Request: r}, nil
	})
	result, err := Run(t.Context(), f, Config{
		VUs: 4, Duration: 500 * time.Millisecond,
		Client: &http.Client{Transport: transport},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed || result.Iterations < 4 || calls.Load() < 4 || tokens.Load() != 1 {
		t.Fatalf("passed=%v iterations=%d calls=%d tokens=%d err=%v", result.Passed, result.Iterations, calls.Load(), tokens.Load(), result.Failures)
	}
}
