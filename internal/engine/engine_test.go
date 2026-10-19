package engine_test

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/gi-runner/gi/internal/demoapi"
	"github.com/gi-runner/gi/internal/engine"
	"github.com/gi-runner/gi/internal/feature"
	"github.com/gi-runner/gi/internal/httpx"
	"github.com/gi-runner/gi/internal/load"
	"github.com/gi-runner/gi/internal/mock"
)

func TestUsersFeatureAgainstDemoAPI(t *testing.T) {
	srv := httptest.NewServer(demoapi.Handler())
	t.Cleanup(srv.Close)
	f := parseExample(t, "users.feature")
	res, err := engine.RunFile(context.Background(), f, engine.Options{
		Vars: map[string]any{"baseUrl": srv.URL},
		HTTP: httpx.New(srv.Client()),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Passed() {
		for _, sc := range res.Scenarios {
			if !sc.Passed {
				t.Errorf("%s\n%s", sc.Name, sc.Error)
			}
		}
	}
}

func TestUsersFeatureAgainstMock(t *testing.T) {
	mockFile := parseExample(t, "users.mock.feature")
	srv := httptest.NewServer(mock.Handler(mockFile))
	t.Cleanup(srv.Close)
	f := parseExample(t, "users.feature")
	res, err := engine.RunFile(context.Background(), f, engine.Options{
		Vars: map[string]any{"baseUrl": srv.URL},
		HTTP: httpx.New(srv.Client()),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Passed() {
		for _, sc := range res.Scenarios {
			if !sc.Passed {
				t.Errorf("%s\n%s", sc.Name, sc.Error)
			}
		}
	}
}

func TestLoadThresholds(t *testing.T) {
	srv := httptest.NewServer(demoapi.Handler())
	t.Cleanup(srv.Close)
	f := parseExample(t, "users.feature")
	res, err := load.Run(context.Background(), f, load.Config{
		VUs:        4,
		Duration:   500 * time.Millisecond,
		Vars:       map[string]any{"baseUrl": srv.URL},
		Client:     srv.Client(),
		Thresholds: []string{"errorRate < 1%"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Requests == 0 {
		t.Fatal("expected requests")
	}
	if res.FailedIterations != 0 {
		t.Fatalf("failed iterations %d, first error: %s", res.FailedIterations, firstErr(res))
	}
	if !res.Passed {
		t.Fatalf("thresholds: %#v", res.Thresholds)
	}
	tight, err := load.Run(context.Background(), f, load.Config{
		VUs:        2,
		Duration:   200 * time.Millisecond,
		Vars:       map[string]any{"baseUrl": srv.URL},
		Client:     srv.Client(),
		Thresholds: []string{"p95 < 1ns"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if tight.Passed {
		t.Fatal("p95 < 1ns should fail")
	}
}

func firstErr(res load.Result) string {
	if len(res.Failures) == 0 {
		return ""
	}
	return res.Failures[0].Error
}

func parseExample(t *testing.T, name string) *feature.File {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	path := filepath.Join(filepath.Dir(file), "..", "..", "examples", name)
	f, err := feature.ParseFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return f
}
