package load

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gi-runner/gi/internal/feature"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDeadlineDoesNotHideEarlierAssertion(t *testing.T) {
	f, err := feature.Parse("load.feature", `Feature: deadline
Scenario: real failure
* url 'http://load.example.test/fast'
* method get
* assert false
Scenario: pending request
* url 'http://load.example.test/pending'
* method get
`)
	if err != nil {
		t.Fatal(err)
	}
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/pending" {
			<-r.Context().Done()
			return nil, r.Context().Err()
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: http.NoBody, Request: r}, nil
	})
	result, err := Run(t.Context(), f, Config{VUs: 1, Duration: 100 * time.Millisecond, Client: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed || result.FailedIterations != 1 || len(result.Failures) != 1 || !strings.Contains(result.Failures[0].Error, "assert failed") {
		t.Fatalf("earlier failure hidden by cancellation: %+v", result)
	}
}

func TestInvalidThresholdNumbers(t *testing.T) {
	for _, expr := range []string{"rps < NaN", "errorRate < Inf", "errorRate < 101%", "p95 < -1ms"} {
		if _, err := ParseThresholds([]string{expr}); err == nil {
			t.Errorf("accepted %q", expr)
		}
	}
}

func TestExternalCancellationIsReturned(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	f, err := feature.Parse("cancel.feature", "Feature: canceled\nScenario: canceled\n* assert true\n")
	if err != nil {
		t.Fatal(err)
	}
	result, err := Run(ctx, f, Config{Duration: time.Millisecond})
	if !errors.Is(err, context.Canceled) || result.Passed {
		t.Fatalf("result passed=%v err=%v", result.Passed, err)
	}
}
