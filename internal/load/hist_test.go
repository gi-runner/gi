package load

import (
	"math"
	"slices"
	"testing"
	"time"
)

func TestHistogramFailsClosedAgainstExactRanks(t *testing.T) {
	exact := &hist{}
	var samples []int64
	for i := int64(1); i <= 31; i++ {
		exact.add(i)
		samples = append(samples, i)
	}
	for _, p := range []float64{0.50, 0.95, 0.99} {
		got := exact.at(p)
		want := nearestRank(samples, p)
		if got != want {
			t.Fatalf("p=%v got %d exact %d below 32ns buckets are exact", p, got, want)
		}
	}

	wide := &hist{}
	samples = samples[:0]
	for i := int64(1); i <= 5000; i++ {
		ns := i * 10_000
		wide.add(ns)
		samples = append(samples, ns)
	}
	for _, p := range []float64{0.50, 0.95, 0.99} {
		got := wide.at(p)
		want := nearestRank(samples, p)
		if got < want {
			t.Fatalf("p=%v histogram %d is under the exact rank %d", p, got, want)
		}
		slack := want/32 + 1
		if got-want > slack {
			t.Fatalf("p=%v histogram %d is more than 1/32 above exact %d", p, got, want)
		}
	}
}

func TestHistStaysInsideMinMax(t *testing.T) {
	var h hist
	h.add(int64(time.Millisecond))
	h.add(int64(5 * time.Millisecond))
	st := h.stats()
	if st.Min != time.Millisecond || st.Max != 5*time.Millisecond {
		t.Fatalf("min %s max %s", st.Min, st.Max)
	}
	for _, d := range []time.Duration{st.P50, st.P95, st.P99} {
		if d < st.Min || d > st.Max {
			t.Fatalf("percentile %s outside %s..%s", d, st.Min, st.Max)
		}
	}
}

func TestHistogramPercentileErrorBound(t *testing.T) {
	samples := make([]int64, 0, 256)
	for bit := range 63 {
		n := int64(1) << uint(bit)
		samples = append(samples, n-1, n)
		if bit < 62 {
			samples = append(samples, n+1)
		}
	}
	samples = append(samples, math.MaxInt64)
	slices.Sort(samples)
	var a, b hist
	for i, n := range samples {
		if i%2 == 0 {
			a.add(n)
		} else {
			b.add(n)
		}
	}
	a.merge(b)
	for _, p := range []float64{0.01, 0.5, 0.95, 0.99} {
		exact := samples[int(math.Ceil(p*float64(len(samples))))-1]
		got := a.at(p)
		if got < exact || got-exact > exact/32+1 {
			t.Errorf("p=%v: %d outside bound for %d", p, got, exact)
		}
	}
}

func nearestRank(samples []int64, p float64) int64 {
	sorted := slices.Clone(samples)
	slices.Sort(sorted)
	rank := int(math.Ceil(p * float64(len(sorted))))
	rank = min(max(rank, 1), len(sorted))
	return sorted[rank-1]
}
