// Package gi runs Karate-style feature files without a JVM.
package gi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"runtime"
	"time"

	"github.com/gi-runner/gi/internal/adapters"
	"github.com/gi-runner/gi/internal/engine"
	"github.com/gi-runner/gi/internal/feature"
	"github.com/gi-runner/gi/internal/httpx"
	"github.com/gi-runner/gi/internal/suite"
)

// Options configures a functional run. A nil Client uses http.DefaultClient.
// Each scenario owns its cookie jar; the client's transport can be shared.
type Options struct {
	Vars    map[string]any
	Env     string
	Tags    string
	Jobs    int
	Timeout time.Duration
	Client  *http.Client
	Output  io.Writer
}

// FileResult is the result of one feature file, including its scenarios and steps.
type FileResult = engine.FileResult

// ScenarioResult describes one executed or skipped scenario.
type ScenarioResult = engine.ScenarioResult

// StepResult describes one step, including nested calls and HTTP exchanges.
type StepResult = engine.StepResult

// Exchange records an HTTP request and response with masked sensitive headers.
type Exchange = engine.Exchange

// Register adds a function to the gi and karate objects for later runs.
// The function receives the JavaScript arguments. Built-in names are rejected.
func Register(name string, fn func([]any) (any, error)) error {
	return engine.Register(name, fn)
}

// Unregister removes a function added by Register.
func Unregister(name string) { engine.Unregister(name) }

// Run executes the named feature files. Failed assertions are represented in the
// results; errors report parsing, setup, or cancellation failures. Check Passed
// on every result, even when err is nil. Directories are not expanded by this API.
func Run(ctx context.Context, paths []string, opt Options) ([]FileResult, error) {
	if len(paths) == 0 {
		return nil, errors.New("at least one feature path is required")
	}
	files := make([]*feature.File, len(paths))
	for i, path := range paths {
		f, err := feature.ParseFile(path)
		if err != nil {
			return nil, err
		}
		files[i] = f
	}
	jobs := opt.Jobs
	if jobs < 1 {
		jobs = runtime.GOMAXPROCS(0)
	}
	sender := httpx.New(opt.Client)
	defer sender.CloseIdleConnections()
	results, err := engine.RunFiles(ctx, files, engine.Options{
		Vars: opt.Vars, Env: opt.Env, Tag: opt.Tags, Jobs: jobs, Timeout: opt.Timeout,
		HTTP: sender, Stdout: opt.Output, Suite: suite.Open(paths[0], opt.Env),
		Dial: adapters.Dial, NewPage: adapters.NewPage,
	}, nil)
	if ctx.Err() != nil {
		return results, ctx.Err()
	}
	if err == nil {
		ran := false
		for _, f := range results {
			for _, sc := range f.Scenarios {
				if !sc.Skipped {
					ran = true
				}
			}
		}
		if !ran {
			return results, errors.New("no scenarios ran")
		}
	}
	return results, err
}
