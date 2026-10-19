package suite

import (
	"context"
	"github.com/gi-runner/gi/internal/value"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestWaitStopsWhenContextEnds(t *testing.T) {
	s := Open(".", "")
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = s.DoOnce(context.Background(), "k", func(context.Context) (any, error) {
			close(started)
			<-release
			return "ok", nil
		})
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("runner did not start")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := s.DoOnce(ctx, "k", func(context.Context) (any, error) { return "unused", nil })
	if err == nil {
		t.Fatal("waiter returned without the context ending")
	}
	close(release)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runner did not finish")
	}
}

func TestCacheOwnsValues(t *testing.T) {
	var s Suite // Zero value must be usable.
	original := map[string]any{"nested": map[string]any{"n": int64(1)}}
	calls := 0
	run := func(context.Context) (any, error) { calls++; return original, nil }
	first, err := s.DoOnce(t.Context(), "value", run)
	if err != nil {
		t.Fatal(err)
	}
	nestedMap(t, first)["n"] = int64(2)
	nestedMap(t, original)["n"] = int64(3)
	second, err := s.DoOnce(t.Context(), "value", run)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || nestedMap(t, second)["n"] != int64(1) {
		t.Fatalf("cached value was mutated: %#v (calls=%d)", second, calls)
	}
}

func TestCacheRejectsRuntimeOwnedValues(t *testing.T) {
	var s Suite
	_, err := s.DoSingle(t.Context(), "function", func(context.Context) (any, error) {
		return map[string]any{"fn": func() {}}, nil
	})
	if err == nil {
		t.Fatal("accepted function shared across runtimes")
	}
}

func nestedMap(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := value.AsMap(v)
	if !ok {
		t.Fatalf("expected object, got %T", v)
	}
	n, ok := value.AsMap(m["nested"])
	if !ok {
		t.Fatalf("expected nested object, got %T", m["nested"])
	}
	return n
}

func TestConfigReadErrorsAreVisible(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "karate-config.js"), 0700); err != nil {
		t.Fatal(err)
	}
	s := Open(filepath.Join(dir, "main.feature"), "")
	if s.Root != dir || s.Err == nil {
		t.Fatalf("root=%s err=%v", s.Root, s.Err)
	}
}

func TestConcurrentCacheDependencyCycle(t *testing.T) {
	var s Suite
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	ready := make(chan struct{}, 2)
	start := make(chan struct{})
	var startOnce sync.Once
	release := func() { startOnce.Do(func() { close(start) }) }
	t.Cleanup(release)
	results := make(chan error, 2)
	for _, keys := range [][2]string{{"a", "b"}, {"b", "a"}} {
		go func() {
			_, err := s.DoOnce(ctx, keys[0], func(nested context.Context) (any, error) {
				ready <- struct{}{}
				<-start
				return s.DoOnce(nested, keys[1], func(context.Context) (any, error) { return "unexpected", nil })
			})
			results <- err
		}()
	}
	for range 2 {
		select {
		case <-ready:
		case <-ctx.Done():
			t.Fatal("cache owners did not start")
		}
	}
	release()
	for range 2 {
		select {
		case err := <-results:
			if err == nil || !strings.Contains(err.Error(), "cyclic cached call") {
				t.Fatalf("cycle was not detected: %v", err)
			}
		case <-ctx.Done():
			t.Fatal("cache owners deadlocked")
		}
	}
}

func TestPanickingCacheOwnerUnblocksWaiters(t *testing.T) {
	var s Suite
	func() {
		defer func() {
			if recover() == nil {
				t.Error("owner panic was swallowed")
			}
		}()
		_, _ = s.DoOnce(t.Context(), "panic", func(context.Context) (any, error) { panic("owner failed") })
	}()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if _, err := s.DoOnce(ctx, "panic", func(context.Context) (any, error) { return "unexpected", nil }); err == nil || ctx.Err() != nil {
		t.Fatalf("failed owner left waiters blocked: %v", err)
	}
}
