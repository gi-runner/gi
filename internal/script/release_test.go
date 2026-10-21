package script

import (
	"context"
	"testing"
)

func TestConfigFailureRunsOnce(t *testing.T) {
	v := New()
	v.Set("n", 0)
	err := v.EvalConfig(`(function(){ n++; throw Error('boom') })()`)
	if err == nil {
		t.Fatal("expected failure")
	}
	got, _ := v.Get("n")
	if got != int64(1) {
		t.Fatalf("executed %v times", got)
	}
}

func TestPathMutationReadsLiveState(t *testing.T) {
	v := New()
	v.Set("obj", map[string]any{"old": 1})
	// A failing expression can still mutate state. set/remove must read the VM,
	// even though Eval returned before its usual synchronization point.
	_, _ = v.Eval(`(function(){obj={fresh:2};throw Error('boom')})()`)
	if err := v.SetPath("obj.extra", 3); err != nil {
		t.Fatal(err)
	}
	got, _ := v.Eval("obj.fresh")
	if got != int64(2) {
		t.Fatalf("lost mutation: %v", got)
	}
}

func TestWatchStopCanReuseRuntime(t *testing.T) {
	v := New()
	for range 100 {
		ctx, cancel := context.WithCancel(t.Context())
		stop, err := v.Watch(ctx)
		if err != nil {
			t.Fatal(err)
		}
		cancel()
		stop()
	}
	got, err := v.Eval("1+1")
	if err != nil || got != int64(2) {
		t.Fatalf("stale interrupt: %v %v", got, err)
	}
}

func FuzzSetPath(f *testing.F) {
	for _, path := range []string{"x[0]", "x[18446744073709551615]", "x.a", "x['escaped']"} {
		f.Add(path)
	}
	f.Fuzz(func(t *testing.T, path string) { v := New(); v.Set("x", map[string]any{}); _ = v.SetPath(path, 1) })
}

func TestExportReadsEachGetterOnce(t *testing.T) {
	v := New()
	got, err := v.Eval(`(function(){let reads=0;return {get value(){return ++reads}}})()`)
	if err != nil {
		t.Fatal(err)
	}
	object, ok := got.(map[string]any)
	if !ok || object["value"] != int64(1) {
		t.Fatalf("export evaluated a getter repeatedly: %v", got)
	}
}

func TestExportGetterFailureIsAnError(t *testing.T) {
	v := New()
	if _, err := v.Eval(`({get value(){throw Error('getter failed')}})`); err == nil {
		t.Fatal("getter exception was lost")
	}
	if err := v.EvalConfig(`({get value(){throw Error('config getter failed')}})`); err == nil {
		t.Fatal("config getter exception was lost")
	}
	got, err := v.Eval("1+1")
	if err != nil || got != int64(2) {
		t.Fatalf("runtime did not recover: %v %v", got, err)
	}
}
