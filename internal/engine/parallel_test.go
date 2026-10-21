package engine

import (
	"sync/atomic"
	"testing"
)

func TestParallelExecutesEveryJobOnceWithinLimit(t *testing.T) {
	const count = 10000
	counts := make([]atomic.Int32, count)
	var active, peak atomic.Int32
	parallel(4, count, func(i int) {
		current := active.Add(1)
		for old := peak.Load(); current > old; old = peak.Load() {
			if peak.CompareAndSwap(old, current) {
				break
			}
		}
		counts[i].Add(1)
		active.Add(-1)
	})
	for i := range counts {
		if got := counts[i].Load(); got != 1 {
			t.Fatalf("job %d ran %d times", i, got)
		}
	}
	if peak.Load() > 4 || active.Load() != 0 {
		t.Fatalf("workers: peak=%d active=%d", peak.Load(), active.Load())
	}
}
