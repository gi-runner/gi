package suite

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestScenarioLocksAreCancellableAndExclusive(t *testing.T) {
	var s Suite
	first, err := s.Acquire(t.Context(), []string{"@lock=database"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Acquire(t.Context(), []string{"@lock=other"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tags := range [][]string{{"@lock=database"}, {"@lock=*"}} {
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
		if release, err := s.Acquire(ctx, tags); !errors.Is(err, context.DeadlineExceeded) {
			if release != nil {
				release()
			}
			t.Errorf("lock escaped held scenario: %v", err)
		}
		cancel()
	}
	first()
	first()
	second()
	exclusive, err := s.Acquire(t.Context(), []string{"@lock=*"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	if release, err := s.Acquire(ctx, nil); !errors.Is(err, context.DeadlineExceeded) {
		if release != nil {
			release()
		}
		t.Errorf("unlocked scenario escaped exclusive lock: %v", err)
	}
	exclusive()
	release, err := s.Acquire(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	release()
}
