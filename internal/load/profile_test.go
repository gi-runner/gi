package load

import (
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gi-runner/gi/internal/feature"
)

func TestClosedLoopRepeats(t *testing.T) {
	f := mustFeature(t, "Feature: loop\nScenario: ok\n* assert true\n")
	result, err := Run(t.Context(), f, Config{VUs: 2, Duration: 400 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if result.Iterations < 4 || result.Profile != "constant users 2" {
		t.Fatalf("iterations=%d profile=%q", result.Iterations, result.Profile)
	}
}

func TestOpenLoopStartsWhileEarlierWorkIsInFlight(t *testing.T) {
	f := mustFeature(t, `Feature: rate
Scenario: held
* url 'http://load.example.test/go'
* method get
* status 200
* url 'http://load.example.test/hold'
* method get
`)
	closedHits, closedPeak := runHeld(t, f, Config{VUs: 1, Duration: time.Second})
	openHits, openPeak := runHeld(t, f, Config{Rate: 10, Duration: time.Second})
	if closedHits != 1 || closedPeak != 1 {
		t.Fatalf("one user issued %d starts, peak %d", closedHits, closedPeak)
	}
	if openHits < 6 || openPeak < 6 {
		t.Fatalf("open loop issued %d starts, peak %d; earlier starts must not block the schedule", openHits, openPeak)
	}
}

func TestRampUsersGrowsThePopulation(t *testing.T) {
	f := mustFeature(t, `Feature: ramp
Scenario: held
* url 'http://load.example.test/hold'
* method get
`)
	_, peak := runHeld(t, f, Config{VUs: 1, VUsTo: 6, UsersToSet: true, Duration: 800 * time.Millisecond})
	if peak < 4 {
		t.Fatalf("peak in flight = %d, want the population to climb", peak)
	}
}

func TestConstantRateCompletesStarts(t *testing.T) {
	var hits atomic.Int64
	f := mustFeature(t, `Feature: fast
Scenario: ok
* url 'http://load.example.test/go'
* method get
* status 200
`)
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		hits.Add(1)
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: http.NoBody, Request: r}, nil
	})
	result, err := Run(t.Context(), f, Config{
		Rate: 10, Duration: time.Second, Client: &http.Client{Transport: transport},
	})
	if err != nil {
		t.Fatal(err)
	}
	if hits.Load() < 8 || hits.Load() > 12 || result.Iterations < 8 {
		t.Fatalf("hits=%d iterations=%d profile=%q", hits.Load(), result.Iterations, result.Profile)
	}
	if result.Profile != "constant rate 10/s" || !result.Open {
		t.Fatalf("profile=%q open=%v peak=%d", result.Profile, result.Open, result.VUs)
	}
	if result.Scheduled.P95 <= 0 || result.CPU < 0 || result.Goroutines < 2 || result.RSS == 0 {
		t.Fatalf("sched=%s cpu=%s rss=%d goroutines=%d", result.Scheduled.P95, result.CPU, result.RSS, result.Goroutines)
	}
}

func TestMaxInFlightCountsMissedStarts(t *testing.T) {
	f := mustFeature(t, `Feature: cap
Scenario: held
* url 'http://load.example.test/hold'
* method get
`)
	var peak atomic.Int64
	var inflight atomic.Int64
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		n := inflight.Add(1)
		defer inflight.Add(-1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		<-r.Context().Done()
		return nil, r.Context().Err()
	})
	result, err := Run(t.Context(), f, Config{
		Rate: 30, Duration: 400 * time.Millisecond, MaxInFlight: 1,
		Client: &http.Client{Transport: transport},
	})
	if err != nil {
		t.Fatal(err)
	}
	if peak.Load() != 1 || result.MissedStarts < 5 || result.VUs != 1 {
		t.Fatalf("peak=%d missed=%d reported peak=%d", peak.Load(), result.MissedStarts, result.VUs)
	}
}

func TestWarmupMissesStayOutsideTheWindow(t *testing.T) {
	f := mustFeature(t, `Feature: warmup
Scenario: held
* url 'http://load.example.test/hold'
* method get
`)
	var peak atomic.Int64
	var inflight atomic.Int64
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		n := inflight.Add(1)
		defer inflight.Add(-1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		<-r.Context().Done()
		return nil, r.Context().Err()
	})
	result, err := Run(t.Context(), f, Config{
		Rate: 10, Warmup: 500 * time.Millisecond, Duration: 200 * time.Millisecond, MaxInFlight: 1,
		Client: &http.Client{Transport: transport},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.MissedStarts != 2 || result.LateStarts != 0 || result.FailedIterations != 2 {
		t.Fatalf("missed=%d late=%d failed=%d", result.MissedStarts, result.LateStarts, result.FailedIterations)
	}
}

func TestSpikeRaisesThenRestoresUsers(t *testing.T) {
	f := mustFeature(t, `Feature: spike
Scenario: held
* url 'http://load.example.test/hold'
* method get
`)
	_, peak := runHeld(t, f, Config{StageText: []string{
		"users=1 duration=150ms",
		"spike users=5 duration=400ms",
	}})
	if peak < 3 {
		t.Fatalf("peak in flight = %d, want the spike to add users", peak)
	}
}

func runHeld(t *testing.T, f *feature.File, cfg Config) (hits, peak int64) {
	t.Helper()
	var started atomic.Int64
	var inflight atomic.Int64
	var maxIn atomic.Int64
	cfg.Client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/go" {
			started.Add(1)
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: http.NoBody, Request: r}, nil
		}
		n := inflight.Add(1)
		defer inflight.Add(-1)
		for {
			p := maxIn.Load()
			if n <= p || maxIn.CompareAndSwap(p, n) {
				break
			}
		}
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}
	if _, err := Run(t.Context(), f, cfg); err != nil {
		t.Fatal(err)
	}
	return started.Load(), maxIn.Load()
}

func mustFeature(t *testing.T, src string) *feature.File {
	t.Helper()
	f, err := feature.Parse("profile.feature", src)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestRestartedUserKeepsIterationSequence(t *testing.T) {
	f := mustFeature(t, `Feature: sequence
Scenario: send
* url 'http://load.example.test/' + vu + '/' + iter
* method get
* pause(10)
`)
	var mu sync.Mutex
	seen := map[string]bool{}
	duplicate := ""
	hits := 0
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		defer mu.Unlock()
		if seen[r.URL.Path] {
			duplicate = r.URL.Path
		}
		seen[r.URL.Path] = true
		hits++
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: http.NoBody, Request: r}, nil
	})}
	_, err := Run(t.Context(), f, Config{Client: client, Stages: []Stage{
		{From: 1, Duration: 150 * time.Millisecond},
		{Spike: true, From: 0, Duration: 100 * time.Millisecond},
		{From: 1, Duration: 150 * time.Millisecond},
	}})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if duplicate != "" || hits < 3 {
		t.Fatalf("duplicate=%q hits=%d", duplicate, hits)
	}
}
