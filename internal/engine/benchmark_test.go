package engine_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gi-runner/gi/internal/engine"
	"github.com/gi-runner/gi/internal/feature"
	"github.com/gi-runner/gi/internal/httpx"
	"github.com/gi-runner/gi/internal/script"
	"github.com/gi-runner/gi/internal/suite"
)

func BenchmarkLoadHTTP(b *testing.B) { benchmarkLoadHTTP(b, "") }

func TestLoadHTTPAllocBudget(t *testing.T) {
	res := testing.Benchmark(BenchmarkLoadHTTP)
	if res.N == 0 {
		t.Fatal("benchmark did not run")
	}
	const budget = 240
	if res.AllocsPerOp() > budget {
		t.Fatalf("load iteration allocs/op = %d, budget is %d", res.AllocsPerOp(), budget)
	}
}

func BenchmarkLoadHTTPAPI(b *testing.B) {
	benchmarkLoadHTTP(b, "* def keys = karate.keysOf({ok:true})\n* match keys == ['ok']\n")
}

func benchmarkLoadHTTP(b *testing.B, extra string) {
	b.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	f, err := feature.Parse("bench.feature", fmt.Sprintf("Feature: bench\nScenario: request\n* url '%s'\n* method get\n* status 200\n* match response.ok == true\n%s", server.URL, extra))
	if err != nil {
		b.Fatal(err)
	}
	sender := httpx.New(server.Client())
	defer sender.CloseIdleConnections()
	vm := script.New()
	release, err := vm.Watch(b.Context())
	if err != nil {
		b.Fatal(err)
	}
	defer release()
	opt := engine.Options{HTTP: sender, LoadMode: true, Jobs: 1, Worker: vm, Suite: suite.Open(f.Path, "")}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		result, err := engine.RunFile(b.Context(), f, opt)
		if err != nil || !result.Passed() {
			b.Fatalf("run: %v %+v", err, result)
		}
	}
}

func TestReusedWorkerFollowsTheURL(t *testing.T) {
	var mu sync.Mutex
	var got []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		got = append(got, r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	vm := script.New()
	release, err := vm.Watch(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	sender := httpx.New(server.Client())
	defer sender.CloseIdleConnections()
	run := func(path string) {
		t.Helper()
		text := fmt.Sprintf("Feature: url\nScenario: get\n* url '%s'\n* path '%s'\n* method get\n* status 200\n", server.URL, path)
		f, err := feature.Parse("url.feature", text)
		if err != nil {
			t.Fatal(err)
		}
		opt := engine.Options{HTTP: sender, LoadMode: true, Jobs: 1, Worker: vm, Suite: suite.Open(f.Path, "")}
		result, err := engine.RunFile(t.Context(), f, opt)
		if err != nil || !result.Passed() {
			t.Fatalf("%s: %v %+v", path, err, result)
		}
	}
	run("one")
	run("two")
	if len(got) != 2 || got[0] != "/one" || got[1] != "/two" {
		t.Fatalf("paths: %v", got)
	}
}
