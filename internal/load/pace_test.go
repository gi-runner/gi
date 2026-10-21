package load

import (
	"math"
	"testing"
	"time"
)

func TestRampRateFollowsTheStageIntegral(t *testing.T) {
	p := newPlan(0, []Stage{{Rate: true, From: 0, To: 10, ToSet: true, Duration: time.Second}})
	var n int
	for {
		due, ok := p.due(n)
		if !ok {
			break
		}
		got := arrivedBy(p, due)
		want := float64(n + 1)
		if math.Abs(got-want) > 1e-6 {
			t.Fatalf("arrival %d at %s has integral %v, want %v", n, due, got, want)
		}
		n++
	}
	if n < 4 || n > 5 {
		t.Fatalf("arrivals = %d, integral over the window is 5", n)
	}
	due, ok := p.due(0)
	if !ok || due == 0 {
		t.Fatal("a ramp from zero injected a start at t=0")
	}
}

func TestRampDownStopsAtTheStageIntegral(t *testing.T) {
	p := newPlan(0, []Stage{{Rate: true, From: 10, To: 0, ToSet: true, Duration: time.Second}})
	n := 0
	for {
		if _, ok := p.due(n); !ok {
			break
		}
		n++
	}
	if n != 5 {
		t.Fatalf("arrivals = %d, want 5", n)
	}
}

func TestRampDownLowersThePopulation(t *testing.T) {
	levels := newPlan(0, []Stage{{From: 4, To: 1, ToSet: true, Duration: 300 * time.Millisecond}}).levels()
	got := map[time.Duration]int{}
	for _, lv := range levels {
		got[lv.At] = lv.N
	}
	if got[0] != 4 || got[100*time.Millisecond] != 3 || got[200*time.Millisecond] != 2 || got[300*time.Millisecond] != 1 {
		t.Fatalf("ramp down: %+v", levels)
	}
}

func TestNormalizeRejectsAMixedProfile(t *testing.T) {
	if _, err := Normalize(Config{VUs: 2, Rate: 10, Duration: time.Second}); err == nil {
		t.Fatal("accepted users and a start rate")
	}
	cfg, err := Normalize(Config{Rate: 10, Duration: time.Second})
	if err != nil || Describe(cfg) != "constant rate 10/s" {
		t.Fatalf("cfg=%+v err=%v", cfg, err)
	}
	cfg, err = Normalize(Config{VUs: 1, VUsTo: 8, UsersToSet: true, Duration: time.Second})
	if err != nil || Describe(cfg) != "ramp users 1..8" {
		t.Fatalf("cfg=%+v err=%v", cfg, err)
	}
	cfg, err = Normalize(Config{RateTo: 4, RateToSet: true, Duration: time.Second})
	if err != nil || Describe(cfg) != "ramp rate 0..4/s" {
		t.Fatalf("cfg=%+v err=%v", cfg, err)
	}
}

func arrivedBy(p plan, t time.Duration) float64 {
	var sum float64
	for _, sp := range p.spans {
		if t <= sp.start {
			continue
		}
		if t >= sp.start+sp.dur {
			sum += sp.area()
			continue
		}
		sec := (t - sp.start).Seconds()
		window := sp.dur.Seconds()
		sum += sp.from*sec + (sp.to-sp.from)*sec*sec/(2*window)
	}
	return sum
}
