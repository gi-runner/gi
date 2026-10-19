package engine

import (
	"context"
	"time"
)

func (e *exec) hook(name string) (StepResult, bool) {
	if e.ctx.Value(setupCall{}) != nil {
		return StepResult{}, false
	}
	start := time.Now()
	ran, err := e.vm.CallHook(name)
	if !ran {
		return StepResult{}, false
	}
	sr := StepResult{Keyword: "*", Text: name, Passed: err == nil, Duration: time.Since(start), Calls: e.calls, Embeds: e.embeds}
	e.calls = nil
	e.embeds = nil
	if err != nil {
		sr.Error = err.Error()
	}
	return sr, true
}

// A file-level hook runs only after its scenario runtime is no longer executing.
func fileHook(ctx context.Context, e *exec, name string, res *ScenarioResult) {
	if e.opt.SkipHooks {
		return
	}
	e.ctx = ctx
	stop, err := e.vm.Watch(ctx)
	if err != nil {
		res.Passed = false
		res.Canceled = true
		res.Error = err.Error()
		return
	}
	defer func() {
		defer stop()
		if err := e.release(); err != nil {
			res.Passed = false
			if res.ReportDisabled {
				res.Error = "scenario failed (report details suppressed)"
			} else {
				res.Error = err.Error()
			}
		}
	}()
	if err := e.bind(); err != nil {
		res.Passed = false
		res.Canceled = ctx.Err() != nil
		res.Error = err.Error()
		return
	}
	if sr, ok := e.hook(name); ok {
		if !res.ReportDisabled {
			res.Steps = append(res.Steps, sr)
		}
		if !sr.Passed {
			res.Passed = false
			if res.ReportDisabled {
				res.Error = "scenario failed (report details suppressed)"
			} else {
				res.Error = sr.Error
			}
		}
		res.Vars = e.vm.Vars()
	}
}
