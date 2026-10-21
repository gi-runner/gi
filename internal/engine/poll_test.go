package engine

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"
)

func TestPollDeadlineBoundsCheckAndInterval(t *testing.T) {
	for _, blocked := range []bool{false, true} {
		synctest.Test(t, func(t *testing.T) {
			start := time.Now()
			calls := 0
			unavailable := errors.New("unavailable")
			err := poll(t.Context(), 25*time.Millisecond, time.Second, nil, func(ctx context.Context) error {
				calls++
				if blocked {
					<-ctx.Done()
				}
				return unavailable
			})
			if !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, unavailable) {
				t.Fatalf("lost deadline or last failure: %v", err)
			}
			if time.Since(start) != 25*time.Millisecond || calls != 1 {
				t.Fatalf("poll took %s with %d checks", time.Since(start), calls)
			}
		})
	}
}

func TestPollDoesNotAcceptSuccessAfterDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		err := poll(t.Context(), time.Millisecond, time.Second, nil, func(ctx context.Context) error { <-ctx.Done(); return nil })
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("accepted late success: %v", err)
		}
	})
}

func TestWaitHTTPRejectsMalformedURLBeforePolling(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := exec{ctx: t.Context()}
		start := time.Now()
		if err := e.waitHTTP(":bad-url", time.Hour, time.Second, nil); err == nil {
			t.Fatal("accepted invalid URL")
		}
		if time.Since(start) != 0 {
			t.Fatal("retried a malformed URL")
		}
	})
}
