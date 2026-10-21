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
	if err := ex.bind(); err != nil {
		return nil, err
	}
	if prepare != nil {
		prepare(ex.vm)
	}
	if err := ex.evalSuite(); err != nil {
		return nil, err
	}
	for _, st := range f.Background {
		sr := ex.runStep(ctx, st)
		if !sr.Passed {
			return nil, fmt.Errorf("mock background: %s", sr.Error)
		}
	}
	return &MockRuntime{ex: ex}, nil
}

func (m *MockRuntime) Run(ctx context.Context, vars map[string]any) ScenarioResult {
	e := m.ex
	stop, err := e.vm.Watch(ctx)
	if err != nil {
		return ScenarioResult{Error: err.Error(), Canceled: true}
	}
	defer stop()
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
