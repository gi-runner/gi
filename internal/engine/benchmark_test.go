package engine_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gi-runner/gi/internal/engine"
	"github.com/gi-runner/gi/internal/feature"
	"github.com/gi-runner/gi/internal/httpx"
	"github.com/gi-runner/gi/internal/suite"
)

func BenchmarkLoadHTTP(b *testing.B) { benchmarkLoadHTTP(b, "") }

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
	opt := engine.Options{HTTP: sender, LoadMode: true, Jobs: 1, Suite: suite.Open(f.Path, "")}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		result, err := engine.RunFile(b.Context(), f, opt)
		if err != nil || !result.Passed() {
			b.Fatalf("run: %v %+v", err, result)
		}
	}
}
