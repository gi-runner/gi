package load

import (
	"testing"
	"time"
)

func TestAbortStopsAFailedThreshold(t *testing.T) {
	parsed, err := ParseThresholds([]string{"errorRate < 1% abort"})
	if err != nil || len(parsed) != 1 || !parsed[0].abort {
		t.Fatalf("parse: %v %+v", err, parsed)
	}
	stopped := false
	s := &session{
		th:         parsed,
		stop:       func() { stopped = true },
		collectors: []collector{{iters: 10}},
		recordAt:   time.Now().Add(-time.Second),
		started:    time.Now().Add(-time.Second),
	}
	s.failures.Store(4)
	s.maybeAbort()
	if !stopped || s.runErr == nil {
		t.Fatalf("stopped %v err %v", stopped, s.runErr)
	}
}

func TestCallSiteThreshold(t *testing.T) {
	const name = "orders.feature:14 GET /orders"
	parsed, err := ParseThresholds([]string{"p95 < 50ms " + name, "errorRate < 1% " + name})
	if err != nil || len(parsed) != 2 || parsed[0].name != name || parsed[1].name != name {
		t.Fatalf("parse: %v %+v", err, parsed)
	}
	if _, err := ParseThresholds([]string{"missed < 1 " + name}); err == nil {
		t.Fatal("accepted a run metric on a call site")
	}
	slow := Result{
		Elapsed: time.Second,
		Endpoints: []Endpoint{{
			Name: name, Count: 10, Errors: 2, RPS: 10,
			Stats: Stats{P95: 80 * time.Millisecond},
		}},
	}
	got := evalThresholds(parsed, slow)
	if len(got) != 2 || got[0].Passed || got[0].Actual != (80*time.Millisecond).String() || got[1].Passed || got[1].Actual != "20.00%" {
		t.Fatalf("%+v", got)
	}
	fast := slow
	fast.Endpoints = []Endpoint{{Name: name, Count: 10, Stats: Stats{P95: 10 * time.Millisecond}, RPS: 10}}
	if pass := evalThresholds(parsed[:1], fast); len(pass) != 1 || !pass[0].Passed {
		t.Fatalf("%+v", pass)
	}
	if missing := evalThresholds(parsed[:1], Result{}); len(missing) != 1 || missing[0].Passed || missing[0].Actual != "absent" {
		t.Fatalf("%+v", missing)
	}
	overall, err := ParseThresholds([]string{"p95 < 50ms"})
	if err != nil || overall[0].name != "" {
		t.Fatal(err)
	}
	whole := evalThresholds(overall, Result{Overall: Stats{P95: 10 * time.Millisecond}})
	if len(whole) != 1 || !whole[0].Passed {
		t.Fatalf("%+v", whole)
	}
}
