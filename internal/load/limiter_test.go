package load

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"
)

func TestRequestLimiterKeepsOnePendingTick(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		limiter := newBucket(10)
		defer limiter.ticker.Stop()
		started := time.Now()
		for n := 1; n <= 2; n++ {
			if err := limiter.wait(t.Context()); err != nil {
				t.Fatal(err)
			}
			if elapsed := time.Since(started); elapsed != time.Duration(n)*100*time.Millisecond {
				t.Fatalf("request %d at %s", n, elapsed)
			}
		}
		synctest.Sleep(time.Second)
		idle := time.Now()
		if err := limiter.wait(t.Context()); err != nil {
			t.Fatal(err)
		}
		if time.Since(idle) != 0 {
			t.Fatal("discarded the pending tick")
		}
		if err := limiter.wait(t.Context()); err != nil {
			t.Fatal(err)
		}
		if elapsed := time.Since(idle); elapsed != 100*time.Millisecond {
			t.Fatalf("accumulated a burst after idle: %s", elapsed)
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if err := limiter.wait(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled limiter: %v", err)
		}
	})
}
