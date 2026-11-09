// Package gi runs feature files for tests, mocks, and load without a JVM.
package gi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gi-runner/gi/internal/adapters"
	"github.com/gi-runner/gi/internal/engine"
	"github.com/gi-runner/gi/internal/feature"
	"github.com/gi-runner/gi/internal/httpx"
	"github.com/gi-runner/gi/internal/mock"
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
	// Steps handle step text that no built-in keyword claims, in order.
	Steps []Step
	// Helpers are added to the karate object for this run only.
	Helpers map[string]func([]any) (any, error)
}

// Step runs Go for step text no built-in keyword claims.
// Pattern is a regexp or a cucumber expression ({string}, {int}, {float}, {word}) of the whole line.
type Step struct {
	Pattern string
	Run     func(ctx context.Context, sc *Scope, args []string) error
}

// Scope gives a Go step the scenario's variables and the step's attachments.
type Scope = engine.Scope

// compile anchors the step patterns and validates the helper names.
func (opt Options) compile() ([]engine.StepDef, error) {
	if err := engine.CheckHelpers(opt.Helpers); err != nil {
		return nil, err
	}
	defs := make([]engine.StepDef, len(opt.Steps))
	for i, s := range opt.Steps {
		if s.Run == nil {
			return nil, fmt.Errorf("step %q has no Run", s.Pattern)
		}
		re, err := compileStep(s.Pattern)
		if err != nil {
			return nil, fmt.Errorf("step %q: %w", s.Pattern, err)
		}
		defs[i] = engine.StepDef{Pattern: re, Run: s.Run}
	}
	return defs, nil
}

func compileStep(pattern string) (*regexp.Regexp, error) {
	if strings.Contains(pattern, "{int}") || strings.Contains(pattern, "{float}") ||
		strings.Contains(pattern, "{word}") || strings.Contains(pattern, "{string}") {
		return compileCucumber(pattern)
	}
	return regexp.Compile(`^(?:` + pattern + `)$`)
}

func compileCucumber(pattern string) (*regexp.Regexp, error) {
	var b strings.Builder
	b.WriteString("^(?:")
	for i := 0; i < len(pattern); {
		switch {
		case strings.HasPrefix(pattern[i:], "{int}"):
			b.WriteString(`(-?\d+)`)
			i += len("{int}")
		case strings.HasPrefix(pattern[i:], "{float}"):
			b.WriteString(`(-?\d+(?:\.\d+)?)`)
			i += len("{float}")
		case strings.HasPrefix(pattern[i:], "{word}"):
			b.WriteString(`(\S+)`)
			i += len("{word}")
		case strings.HasPrefix(pattern[i:], "{string}"):
			b.WriteString(`"([^"]*)"`)
			i += len("{string}")
		default:
			b.WriteString(regexp.QuoteMeta(pattern[i : i+1]))
			i++
		}
	}
	b.WriteString(")$")
	return regexp.Compile(b.String())
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

// Run executes the named feature files. Assertion failures are on the results.
// Errors are parse, setup, or cancellation. Check Passed on every result. Paths are files.
func Run(ctx context.Context, paths []string, opt Options) ([]FileResult, error) {
	if len(paths) == 0 {
		return nil, errors.New("at least one feature path is required")
	}
	steps, err := opt.compile()
	if err != nil {
		return nil, err
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
		Dial: adapters.Dial, NewPage: adapters.NewPage, StartMock: mock.Start,
		Steps: steps, Helpers: opt.Helpers,
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

// Finding is a step, configure key, or tag that gi cannot run as written.
type Finding = engine.Finding

// Check parses the feature files and reports what gi cannot run, without
// running anything. Steps in opt count as defined.
func Check(paths []string, opt Options) ([]Finding, error) {
	steps, err := opt.compile()
	if err != nil {
		return nil, err
	}
	var out []Finding
	for _, path := range paths {
		f, err := feature.ParseFile(path)
		if err != nil {
			return out, err
		}
		out = append(out, engine.Check(f, steps)...)
	}
	return out, nil
}

// Test runs the feature files and reports each scenario as a subtest named
// feature/scenario. Output defaults to t.Output().
func Test(t *testing.T, opt Options, paths ...string) {
	t.Helper()
	if opt.Output == nil {
		opt.Output = t.Output()
	}
	results, err := Run(t.Context(), paths, opt)
	for _, f := range results {
		t.Run(f.Name, func(t *testing.T) {
			for _, sc := range f.Scenarios {
				t.Run(sc.Name, func(t *testing.T) {
					switch {
					case sc.Skipped:
						t.Skip("not selected")
					case !sc.Passed:
						t.Error(sc.Error)
					}
				})
			}
		})
	}
	if err != nil {
		t.Fatal(err)
	}
}
