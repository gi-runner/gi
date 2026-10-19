package script

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/grafana/sobek"
)

func TestRuntimeLeaseCancellationAndReuse(t *testing.T) {
	v := New()
	stop, err := v.Watch(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		release, err := v.Watch(ctx)
		if err == nil {
			release()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("lease: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("lease acquisition ignored cancellation")
	}
	stop()
	stop, err = v.Watch(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	got, err := v.Eval("1+1")
	if err != nil || got != int64(2) {
		t.Fatalf("reuse: %v %v", got, err)
	}
}

func TestReentrantWatchKeepsOuterInterrupt(t *testing.T) {
	v := New()
	outer, err := v.Watch(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer outer()
	v.SetFunction("hook", func(sobek.FunctionCall) sobek.Value {
		inner, err := v.Watch(v.ctx)
		if err != nil {
			panic(v.rt.NewGoError(err))
		}
		// The outer execution is still on the stack. A nested lease must not
		// clear the interrupt that already belongs to that execution.
		v.rt.Interrupt(context.Canceled)
		inner()
		return sobek.Undefined()
	})
	done := make(chan error, 1)
	go func() {
		_, err := v.Eval("hook(); while(true){}")
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("loop kept running after cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("reentrant watch cleared the caller's interrupt")
	}
}

func TestCachedClosureCallsAreSerialized(t *testing.T) {
	origin := New()
	origin.Set("n", int64(0))
	fn, err := origin.Eval("function(){ n++; return n }")
	if err != nil {
		t.Fatal(err)
	}
	sealed := origin.Seal(fn)
	const count = 32
	results := make(chan any, count)
	errs := make(chan error, count)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	for range count {
		wg.Go(func() {
			caller := New()
			stop, err := caller.Watch(ctx)
			if err != nil {
				errs <- err
				return
			}
			defer stop()
			caller.Set("f", sealed)
			got, err := caller.Eval("f()")
			if err != nil {
				errs <- err
				return
			}
			results <- got
		})
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	seen := map[any]bool{}
	for got := range results {
		if seen[got] {
			t.Errorf("duplicate counter: %v", got)
		}
		seen[got] = true
	}
	if len(seen) != count || !seen[int64(count)] {
		t.Fatalf("counter values: %v", seen)
	}
}
