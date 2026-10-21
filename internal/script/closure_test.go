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

func TestParkedCachedCallUsesCallerDeadline(t *testing.T) {
	origin := New()
	fn, err := origin.Eval("function(){ while(true){} }")
	if err != nil {
		t.Fatal(err)
	}
	sealed := origin.Seal(fn)
	ownerCtx, cancelOwner := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancelOwner()
	ready := make(chan struct{})
	ownerDone := make(chan struct{})
	origin.SetFunction("hold", func(sobek.FunctionCall) sobek.Value {
		signalled := false
		err := origin.Park(func(wake <-chan struct{}) (bool, error) {
			if !signalled {
				close(ready)
				signalled = true
			}
			select {
			case <-wake:
				return true, nil
			case <-ownerCtx.Done():
				return false, ownerCtx.Err()
			}
		})
		if err != nil {
			panic(origin.rt.NewGoError(err))
		}
		return sobek.Undefined()
	})
	go func() {
		defer close(ownerDone)
		stop, err := origin.Watch(ownerCtx)
		if err != nil {
			return
		}
		defer stop()
		_, _ = origin.Eval("hold()")
	}()
	defer func() { cancelOwner(); <-ownerDone }()
	<-ready
	caller := New()
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Millisecond)
	defer cancel()
	stop, err := caller.Watch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	caller.Set("f", sealed)
	started := time.Now()
	_, err = caller.Eval("f()")
	if err == nil {
		t.Fatal("cached infinite loop succeeded")
	}
	if time.Since(started) > 500*time.Millisecond {
		t.Fatal("parked cached call ignored caller deadline")
	}
}

func TestCancelledInitializationReturnsError(t *testing.T) {
	v := New()
	ctx, cancel := context.WithCancel(t.Context())
	stop, err := v.Watch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	cancel()
	// Force the same interruption that can arrive during helper initialization.
	v.rt.Interrupt(context.Canceled)
	if err := v.Bind(Bindings{}); err == nil {
		t.Fatal("cancelled helper initialization succeeded")
	}
}

func TestNestedParkRestoresOuterHandoff(t *testing.T) {
	origin := New()
	ownerCtx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	ready := make(chan struct{})
	done := make(chan struct{})
	origin.SetFunction("hold", func(sobek.FunctionCall) sobek.Value {
		first := true
		_ = origin.Park(func(wake <-chan struct{}) (bool, error) {
			if first {
				close(ready)
				first = false
			}
			select {
			case <-wake:
				return true, nil
			case <-ownerCtx.Done():
				return false, ownerCtx.Err()
			}
		})
		return sobek.Undefined()
	})
	origin.SetFunction("inner", func(sobek.FunctionCall) sobek.Value {
		timer := time.NewTimer(10 * time.Millisecond)
		defer timer.Stop()
		_ = origin.Park(func(wake <-chan struct{}) (bool, error) {
			select {
			case <-wake:
				return true, nil
			case <-timer.C:
				return false, nil
			}
		})
		return sobek.Undefined()
	})
	functions := make([]any, 0, 2)
	for _, src := range []string{"function(){inner();return 1}", "function(){return 2}"} {
		fn, err := origin.Eval(src)
		if err != nil {
			t.Fatal(err)
		}
		functions = append(functions, origin.Seal(fn))
	}
	go func() {
		defer close(done)
		stop, err := origin.Watch(ownerCtx)
		if err != nil {
			return
		}
		defer stop()
		_, _ = origin.Eval("hold()")
	}()
	defer func() { cancel(); <-done }()
	select {
	case <-ready:
	case <-ownerCtx.Done():
		t.Fatal("owner did not park")
	}
	for i, fn := range functions {
		caller := New()
		ctx, stopCtx := context.WithTimeout(t.Context(), 500*time.Millisecond)
		stop, err := caller.Watch(ctx)
		if err != nil {
			stopCtx()
			t.Fatal(err)
		}
		caller.Set("f", fn)
		got, err := caller.Eval("f()")
		stop()
		stopCtx()
		if err != nil || got != int64(i+1) {
			t.Fatalf("call %d: %v %v", i, got, err)
		}
	}
}
