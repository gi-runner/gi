package engine

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gi-runner/gi/internal/feature"
	"github.com/gi-runner/gi/internal/script"
	"github.com/gi-runner/gi/internal/suite"
	"github.com/gi-runner/gi/internal/value"
)

// MockRuntime owns a persistent scenario VM and runs one request at a time.
// A cancelled request returns without waiting for the active one.
type MockRuntime struct{ ex *exec }

func NewMockRuntime(ctx context.Context, f *feature.File, prepare func(*script.VM)) (*MockRuntime, error) {
	ex := newExec(ctx, f, Options{Mock: true, Suite: suite.Open(f.Path, "")})
	stop, err := ex.vm.Watch(ctx)
	if err != nil {
		return nil, err
	}
	defer stop()
	if err := ex.vm.Try(func() error { return ex.initMock(prepare) }); err != nil {
		return nil, err
	}
	return &MockRuntime{ex: ex}, nil
}

func (ex *exec) initMock(prepare func(*script.VM)) error {
	if err := ex.bind(); err != nil {
		return err
	}
	if prepare != nil {
		prepare(ex.vm)
	}
	if err := ex.evalSuite(); err != nil {
		return err
	}
	for _, st := range ex.file.Background {
		sr := ex.runStep(ex.ctx, st)
		if !sr.Passed {
			return fmt.Errorf("mock background: %s", sr.Error)
		}
	}
	return nil
}

func (m *MockRuntime) Run(ctx context.Context, vars map[string]any) ScenarioResult {
	e := m.ex
	stop, err := e.vm.Watch(ctx)
	if err != nil {
		return ScenarioResult{Error: err.Error(), Canceled: true}
	}
	defer stop()
	var res ScenarioResult
	if err := e.vm.Try(func() error {
		res = m.run(ctx, vars)
		return nil
	}); err != nil {
		return ScenarioResult{Error: err.Error(), Canceled: ctx.Err() != nil}
	}
	return res
}

func (m *MockRuntime) run(ctx context.Context, vars map[string]any) ScenarioResult {
	e := m.ex
	e.ctx = ctx
	for _, k := range []string{"response", "responseStatus", "responseHeaders", "responseDelay", "pathParams"} {
		e.vm.Set(k, value.Absent{})
	}
	for k, v := range vars {
		e.vm.Set(k, v)
	}
	for _, sc := range e.file.Scenarios {
		if sc.IsSetup || feature.HasTag(sc.Tags, "ignore") {
			continue
		}
		result := ScenarioResult{Name: sc.Name, Passed: true, Started: time.Now()}
		if strings.Contains(sc.Name, "(") {
			ok, err := e.vm.Truthy(sc.Name, nil, nil)
			if err != nil {
				return ScenarioResult{Error: fmt.Sprintf("mock matcher %q: %v", sc.Name, err)}
			}
			if !ok {
				continue
			}
		}
		e.miss = false
		for _, st := range sc.Steps[sc.Lead:] {
			if err := ctx.Err(); err != nil {
				return ScenarioResult{Error: err.Error()}
			}
			sr := e.runStep(ctx, st)
			result.Steps = append(result.Steps, sr)
			if !sr.Passed {
				result.Passed = false
				result.Error = sr.Error
				break
			}
		}
		if e.miss {
			continue
		}
		result.Vars = e.vm.Vars()
		result.Duration = time.Since(result.Started)
		return result
	}
	return ScenarioResult{Miss: true}
}
