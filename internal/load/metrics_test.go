package load

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/gi-runner/gi/internal/engine"
)

func TestEndpointLimitPreservesEverySample(t *testing.T) {
	s := session{cfg: Config{MaxEndpoints: 3}}
	s.initMetrics()
	// Establish stable labels before concurrent arrivals race to fill the registry.
	for i := range 3 {
		s.record(engine.Exchange{Endpoint: fmt.Sprintf("GET /kept-%c", 'a'+i), Duration: time.Millisecond})
	}
	var wg sync.WaitGroup
	for i := range 1000 {
		wg.Go(func() {
			err := ""
			if i%2 == 0 {
				err = "transport failed"
			}
			s.record(engine.Exchange{Endpoint: fmt.Sprintf("GET /random-%x", i), Duration: 2 * time.Millisecond, Error: err})
		})
	}
	wg.Wait()
	locals := make([]*collector, len(s.collectors))
	histograms := 0
	for i := range s.collectors {
		locals[i] = &s.collectors[i]
		histograms += len(s.collectors[i].samples)
	}
	result := merge(locals, time.Second)
	if len(s.endpoints.names) != 3 || len(result.Endpoints) != 4 || histograms != 4 {
		t.Fatalf("unbounded or duplicated histograms: names=%d endpoints=%d histograms=%d", len(s.endpoints.names), len(result.Endpoints), histograms)
	}
	var count int64
	for _, endpoint := range result.Endpoints {
		count += endpoint.Count
		if endpoint.Name == overflowEndpoint && (endpoint.Count != 1000 || endpoint.Errors != 500 || endpoint.Stats.Min != 2*time.Millisecond) {
			t.Fatalf("lost overflow samples: %+v", endpoint)
		}
	}
	if count != 1003 || result.TransportErrors != 500 || s.endpoints.overflow.Load() != 1000 {
		t.Fatalf("lost totals: requests=%d transport=%d overflow=%d", count, result.TransportErrors, s.endpoints.overflow.Load())
	}
	if result.Overall.Min != time.Millisecond || result.Overall.Max != 2*time.Millisecond {
		t.Fatalf("changed overall latency: %+v", result.Overall)
	}
}

func TestConcurrentLabelsAndRepeatedKnownEndpoint(t *testing.T) {
	s := session{cfg: Config{MaxEndpoints: 7}}
	s.initMetrics()
	var wg sync.WaitGroup
	for i := range 1000 {
		wg.Go(func() {
			s.record(engine.Exchange{Endpoint: fmt.Sprintf("GET /path-%c", 'a'+i%20), Duration: time.Millisecond})
		})
	}
	wg.Wait()
	if len(s.endpoints.names) != 7 {
		t.Fatalf("registry has %d labels", len(s.endpoints.names))
	}
	overflow := s.endpoints.overflow.Load()
	for name := range s.endpoints.names {
		s.record(engine.Exchange{Endpoint: name, Duration: time.Millisecond})
	}
	if s.endpoints.overflow.Load() != overflow {
		t.Fatal("known names counted as overflow after limit")
	}
}

func BenchmarkEndpointMetrics(b *testing.B) {
	names := make([]string, 32)
	for i := range names {
		names[i] = fmt.Sprintf("GET /path-%c", 'a'+i)
	}
	for _, shared := range []bool{false, true} {
		b.Run(fmt.Sprintf("shared=%t", shared), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if shared {
					s := session{cfg: Config{MaxEndpoints: 1000}}
					s.initMetrics()
					for range 16 {
						for _, name := range names {
							s.record(engine.Exchange{Endpoint: name, Duration: time.Millisecond})
						}
					}
				} else {
					for range 16 {
						collector := &collector{samples: make(map[string]*hist)}
						for _, name := range names {
							collector.add(engine.Exchange{Endpoint: name, Duration: time.Millisecond})
						}
					}
				}
			}
		})
	}
}
