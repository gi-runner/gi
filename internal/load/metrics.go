package load

import (
	"hash/maphash"
	"math"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

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
	s.collectors[shard].note(s.slot(time.Now()), ex)
}

func (s *session) slot(now time.Time) int {
	span := s.end.Sub(s.recordAt)
	if span <= 0 {
		return -1
	}
	off := now.Sub(s.recordAt)
	if off < 0 || !now.Before(s.end) {
		return -1
	}
	n := int(int64(off) * int64(timelineSlots) / int64(span))
	if n < 0 {
		return 0
	}
	if n >= timelineSlots {
		return timelineSlots - 1
	}
	return n
}

func usersOf(n uint64) int {
	if n > uint64(math.MaxInt) {
		return math.MaxInt
	}
	return int(n)
}

func (s *session) noteUsers() {
	n := s.slot(time.Now())
	if n < 0 {
		return
	}
	cur := uint64(max(s.inflight.Load(), 0))
	for {
		prev := s.userPeak[n].Load()
		if cur <= prev || s.userPeak[n].CompareAndSwap(prev, cur) {
			return
		}
	}
}

func (s *session) timeline(elapsed time.Duration) []Slot {
	span := s.end.Sub(s.recordAt)
	if span <= 0 {
		span = elapsed
	}
	if span <= 0 {
		return nil
	}
	var slots [timelineSlots]sampleSlot
	saw := false
	for i := range s.collectors {
		s.collectors[i].mu.Lock()
		for b, slot := range s.collectors[i].slots {
			if slot.lat.n == 0 {
				continue
			}
			saw = true
			slots[b].lat.merge(slot.lat)
			slots[b].errN += slot.errN
		}
		s.collectors[i].mu.Unlock()
	}
	for i := range s.userPeak {
		if s.userPeak[i].Load() > 0 {
			saw = true
			break
		}
	}
	if !saw {
		return nil
	}
	width := span / timelineSlots
	out := make([]Slot, timelineSlots)
	for i := range out {
		stats := slots[i].lat.stats()
		out[i] = Slot{
			Offset:   time.Duration(i) * width,
			Requests: slots[i].lat.n,
			Errors:   slots[i].errN,
			Mean:     stats.Mean,
			P95:      stats.P95,
			Users:    usersOf(s.userPeak[i].Load()),
		}
	}
	return out
}
