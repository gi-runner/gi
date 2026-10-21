package load

import (
	"math"
	"testing"
	"time"
)

func TestStagePlanRampsHoldsAndSpikes(t *testing.T) {
	stages := []Stage{
		{From: 1, To: 4, ToSet: true, Duration: 300 * time.Millisecond},
		{From: 4, Duration: 100 * time.Millisecond},
		{Spike: true, From: 8, Duration: 100 * time.Millisecond},
	}
	levels := plan{stages: stages}.levels()
	got := map[time.Duration]int{}
	for _, lv := range levels {
		got[lv.At] = lv.N
	}
	if got[0] != 1 || got[100*time.Millisecond] != 2 || got[300*time.Millisecond] != 4 {
		t.Fatalf("ramp levels: %+v", levels)
	}
	if got[400*time.Millisecond] != 8 || got[500*time.Millisecond] != 4 {
		t.Fatalf("spike did not jump and return: %+v", levels)
	}
}

func TestStageRateMatchesTheSinglePacer(t *testing.T) {
	p := newPlan(0, []Stage{{Rate: true, From: 3, To: 3, Duration: time.Second}})
	var n int
	for {
		due, ok := p.due(n)
		if !ok {
			break
		}
		want := time.Duration(float64(n) / 3 * float64(time.Second))
		if due != want {
			t.Fatalf("arrival %d at %s, want %s", n, due, want)
		}
		n++
	}
	if n != 3 {
		t.Fatalf("arrivals = %d, want 3", n)
	}
}

func TestSpikeRateIsARectangularPulse(t *testing.T) {
	p := newPlan(0, []Stage{
		{Rate: true, From: 10, Duration: time.Second},
		{Rate: true, Spike: true, From: 100, Duration: 100 * time.Millisecond},
		{Rate: true, From: 10, Duration: time.Second},
	})
	// 10 over the first second, 100 over the next 100ms, 10 over the last second.
	var n int
	for {
		if _, ok := p.due(n); !ok {
			break
		}
		n++
	}
	if n < 28 || n > 32 {
		t.Fatalf("arrivals = %d, want about 30", n)
	}
	spans := p.spans
	if len(spans) != 3 || math.Abs(spans[1].area()-10) > 1e-9 {
		t.Fatalf("spike area = %v", spans[1].area())
	}
}

func TestParseStageRejectsAMixedShape(t *testing.T) {
	if _, err := ParseStage("spike users=1:4 duration=1s"); err == nil {
		t.Fatal("accepted a ranged spike")
	}
	st, err := ParseStage("users=1:40 duration=30s")
	if err != nil || !st.ToSet || st.From != 1 || st.To != 40 {
		t.Fatalf("%+v %v", st, err)
	}
	cfg, err := Normalize(Config{StageText: []string{"rate=10 duration=1s", "spike rate=50 duration=1s"}, MaxInFlight: 4})
	if err != nil || Describe(cfg) != "constant rate 10/s then spike rate 50/s" {
		t.Fatalf("%+v %v", cfg, err)
	}
}

func TestStageLevelsAndDurationsAreCheckedOnce(t *testing.T) {
	if _, err := ParseStage("rate=Inf duration=1s"); err == nil {
		t.Fatal("accepted an infinite rate")
	}
	if _, err := Normalize(Config{Stages: []Stage{{From: -1, Duration: time.Second}}}); err == nil {
		t.Fatal("accepted a negative user level")
	}
	if _, err := Normalize(Config{Stages: []Stage{{Rate: true, From: math.Inf(1), Duration: time.Second}}}); err == nil {
		t.Fatal("accepted an infinite stage on Config")
	}
	huge := time.Duration(math.MaxInt64 / 2)
	_, err := Normalize(Config{Stages: []Stage{
		{From: 1, Duration: huge},
		{From: 1, Duration: huge},
		{From: 1, Duration: huge},
	}})
	if err == nil {
		t.Fatal("accepted a duration that overflows")
	}
}
