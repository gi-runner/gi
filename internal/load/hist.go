package load

import (
	"math"
	"math/bits"
	"time"
)

// hist retains 32 buckets per power of two, with exact buckets below 32 ns.
// Memory is bounded and percentile upper bounds have at most 1/32 relative error.
type hist struct {
	n, min, max int64
	sum         float64
	bucket      [59 * 32]uint64
}

func (h *hist) add(ns int64) {
	if ns < 0 {
		ns = 0
	}
	if h.n == 0 || ns < h.min {
		h.min = ns
	}
	if ns > h.max {
		h.max = ns
	}
	h.n++
	h.sum += float64(ns)
	index := int(ns)
	if ns >= 32 {
		exponent := bits.Len64(uint64(ns)) - 1
		index = (exponent-4)*32 + int((uint64(ns)>>uint(exponent-5))&31)
	}
	h.bucket[index]++
}

func (h *hist) merge(o hist) {
	if o.n == 0 {
		return
	}
	if h.n == 0 || o.min < h.min {
		h.min = o.min
	}
	if o.max > h.max {
		h.max = o.max
	}
	h.n += o.n
	h.sum += o.sum
	for i := range h.bucket {
		h.bucket[i] += o.bucket[i]
	}
}

func (h *hist) stats() Stats {
	if h.n == 0 {
		return Stats{}
	}
	return Stats{
		Min:  time.Duration(h.min),
		Max:  time.Duration(h.max),
		Mean: time.Duration(h.sum / float64(h.n)),
		P50:  time.Duration(h.at(0.50)),
		P95:  time.Duration(h.at(0.95)),
		P99:  time.Duration(h.at(0.99)),
	}
}

// at reports the upper edge of the bucket that holds the nearest rank.
// Latency thresholds therefore fail closed when a sample sits in a wide bucket.
func (h *hist) at(p float64) int64 {
	if h.n == 0 {
		return 0
	}
	if p <= 0 {
		return h.min
	}
	if p >= 1 {
		return h.max
	}
	rank := uint64(max(math.Ceil(p*float64(h.n)), 1))
	var seen uint64
	for i, c := range h.bucket {
		seen += c
		if seen < rank {
			continue
		}
		hi := int64(i)
		if i >= 32 {
			shift := uint(i/32 - 1)
			hi = (int64(32+i%32) << shift) | ((int64(1) << shift) - 1)
		}
		if hi > h.max {
			return h.max
		}
		if hi < h.min {
			return h.min
		}
		return hi
	}
	return h.max
}
