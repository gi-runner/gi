package load

import (
	"testing"
	"time"

	"github.com/gi-runner/gi/internal/engine"
)

func TestTimelineSplitsTheWindow(t *testing.T) {
	start := time.Now()
	s := session{recordAt: start, end: start.Add(30 * time.Second), cfg: Config{MaxEndpoints: 10}}
	s.initMetrics()
	s.collectors[0].note(0, engine.Exchange{Duration: time.Millisecond, Error: "down"})
	s.collectors[0].note(2, engine.Exchange{Duration: time.Millisecond})
	s.collectors[0].note(2, engine.Exchange{Duration: 100 * time.Millisecond})
	s.collectors[0].note(timelineSlots-1, engine.Exchange{Duration: 3 * time.Millisecond})
	if len(s.collectors) > 1 {
		s.collectors[1].note(0, engine.Exchange{Duration: time.Millisecond})
	}
	s.userPeak[0].Store(4)
	s.userPeak[timelineSlots-1].Store(9)

	got := s.timeline(30 * time.Second)
	if len(got) != timelineSlots {
		t.Fatalf("slots = %d", len(got))
	}
	wantFirst := int64(1)
	if len(s.collectors) > 1 {
		wantFirst = 2
	}
	if got[0].Requests != wantFirst || got[0].Errors != 1 || got[0].Users != 4 || got[0].Mean != time.Millisecond || got[0].P95 != time.Millisecond {
		t.Fatalf("first slice = %+v", got[0])
	}
	last := got[timelineSlots-1]
	if last.Requests != 1 || last.Errors != 0 || last.Users != 9 || last.Mean != 3*time.Millisecond || last.P95 != 3*time.Millisecond {
		t.Fatalf("last slice = %+v", last)
	}
	if got[1].Requests != 0 || got[1].Users != 0 {
		t.Fatalf("empty slice = %+v", got[1])
	}
	if got[2].Requests != 2 || got[2].P95 != 100*time.Millisecond || got[2].Mean >= got[2].P95 {
		t.Fatalf("tail hidden by the mean: %+v", got[2])
	}
}

func TestTimelineSkipsARunWithNoWindow(t *testing.T) {
	s := session{cfg: Config{MaxEndpoints: 10}}
	s.initMetrics()
	s.record(engine.Exchange{Endpoint: "GET /a", Duration: time.Millisecond})
	if s.timeline(time.Second) != nil {
		t.Fatal("recorded a timeline outside a measured window")
	}
}
