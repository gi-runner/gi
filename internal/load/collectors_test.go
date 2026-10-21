package load

import (
	"sync"
	"testing"
	"time"

	"github.com/gi-runner/gi/internal/engine"
)

func TestConcurrentCollectorTotals(t *testing.T) {
	collector := &local{samples: map[string]*hist{}}
	var wg sync.WaitGroup
	for i := range 1000 {
		wg.Go(func() {
			failed := i%2 == 0
			message := ""
			if failed {
				message = "transport failed"
			}
			collector.add(engine.Exchange{Endpoint: "GET /test", Duration: time.Millisecond, Error: message})
			collector.addSched(2 * time.Millisecond)
			collector.recordIteration(failed)
		})
	}
	wg.Wait()
	result := merge([]*local{collector}, time.Second)
	if result.Iterations != 1000 || collector.failIters != 500 || result.TransportErrors != 500 || len(result.Endpoints) != 1 || result.Endpoints[0].Count != 1000 {
		t.Fatalf("lost concurrent metrics: iterations=%d failures=%d transport=%d endpoints=%+v", result.Iterations, collector.failIters, result.TransportErrors, result.Endpoints)
	}
	if result.Overall.P95 != time.Millisecond || result.Scheduled.P95 != 2*time.Millisecond {
		t.Fatalf("changed latency metrics: %+v %+v", result.Overall, result.Scheduled)
	}
}
