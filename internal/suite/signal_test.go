package suite

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMailboxQueueAndCancellation(t *testing.T) {
	s := Open(t.TempDir(), "")
	original := map[string]any{"id": int64(7)}
	if err := s.Signal(original); err != nil {
		t.Fatal(err)
	}
	original["id"] = int64(99)
	got, err := s.Listen(t.Context(), 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	m, ok := got.(map[string]any)
	if !ok || m["id"] != int64(7) {
		t.Fatalf("signal was not copied: %#v", got)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.Listen(ctx, time.Hour, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	if _, err := s.Listen(t.Context(), 0, nil); err == nil || !strings.Contains(err.Error(), "listen timed out") {
		t.Fatalf("timeout: %v", err)
	}
}

func TestMailboxManyWaitersAndWakeups(t *testing.T) {
	s := Open(t.TempDir(), "")
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	const count = 64
	results := make(chan any, count)
	errs := make(chan error, count)
	var wg sync.WaitGroup
	for range count {
		wg.Go(func() {
			got, err := s.Listen(ctx, time.Second, nil)
			if err != nil {
				errs <- err
				return
			}
			results <- got
		})
	}
	for i := range count {
		if err := s.Signal(strconv.Itoa(i)); err != nil {
			t.Fatal(err)
		}
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	seen := map[any]bool{}
	for result := range results {
		if seen[result] {
			t.Errorf("duplicate signal %v", result)
		}
		seen[result] = true
	}
	if len(seen) != count {
		t.Fatalf("received %d signals", len(seen))
	}
}

func TestMailboxTimeoutAndCancelRaces(t *testing.T) {
	// Every waiter has an independent watchdog, so a lost wakeup cannot hang CI.
	for range 200 {
		s := Open(t.TempDir(), "")
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)
		go func() { _, err := s.Listen(ctx, time.Hour, nil); done <- err }()
		cancel()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			t.Fatal("cancelled listener stuck")
		}
		done = make(chan error, 1)
		go func() { _, err := s.Listen(t.Context(), time.Nanosecond, nil); done <- err }()
		select {
		case err := <-done:
			if err == nil || !strings.Contains(err.Error(), "listen timed out") {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			t.Fatal("expired listener stuck")
		}
	}
}
