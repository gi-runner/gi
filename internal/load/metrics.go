package load

import (
	"hash/maphash"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/gi-runner/gi/internal/engine"
)

const overflowEndpoint = "[other endpoints]"

type endpointNames struct {
	mu       sync.RWMutex
	names    map[string]struct{}
	limit    int
	overflow atomic.Int64
}

func (n *endpointNames) label(name string) string {
	n.mu.RLock()
	_, known := n.names[name]
	full := len(n.names) >= n.limit
	n.mu.RUnlock()
	if known {
		return name
	}
	if !full {
		n.mu.Lock()
		_, known = n.names[name]
		if !known && len(n.names) < n.limit {
			n.names[name] = struct{}{}
			known = true
		}
		n.mu.Unlock()
		if known {
			return name
		}
	}
	n.overflow.Add(1)
	return overflowEndpoint
}

func (s *session) initMetrics() {
	shards := min(runtime.GOMAXPROCS(0), 32)
	if len(s.plan.stages) > 0 && !s.plan.rate() {
		shards = min(shards, s.plan.maxUsers())
	}
	if s.plan.rate() && s.cfg.MaxInFlight > 0 {
		shards = min(shards, s.cfg.MaxInFlight)
	}
	s.collectors = make([]collector, shards)
	for i := range s.collectors {
		s.collectors[i].samples = make(map[string]*hist)
	}
	s.endpoints = endpointNames{names: make(map[string]struct{}), limit: s.cfg.MaxEndpoints}
	s.metricSeed = maphash.MakeSeed()
}

func (s *session) record(ex engine.Exchange) {
	ex.Endpoint = s.endpoints.label(ex.Endpoint)
	// A label always belongs to one shard, so users cannot multiply its histogram.
	shard := maphash.String(s.metricSeed, ex.Endpoint) % uint64(len(s.collectors))
	s.collectors[shard].add(ex)
}
