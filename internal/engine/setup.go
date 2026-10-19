package engine

import (
	"context"
	"errors"
	"fmt"
	"maps"

	"github.com/gi-runner/gi/internal/feature"
)

// setupCall marks a scenario started by gi.setup. Hooks do not run inside it.
type setupCall struct{}

func (e *exec) setup(name string, once bool) (any, error) {
	if e.ctx.Value(setupCall{}) != nil {
		return nil, errors.New("setup cannot call setup")
	}
	sc, ok := e.file.Setup(name)
	if !ok {
		if name == "" {
			return nil, errors.New("no scenario with @setup")
		}
		return nil, fmt.Errorf("no @setup scenario named %q", name)
	}
	run := func(ctx context.Context) (any, error) {
		body := sc
		if sc.Lead > 0 && sc.Lead <= len(sc.Steps) {
			body.Steps = append([]feature.Step{}, sc.Steps[sc.Lead:]...)
		}
		restore := e.vm.Scope(false)
		defer restore()
		opt := e.callOptions(e.stack)
		res := runScenario(context.WithValue(ctx, setupCall{}, true), e.file, body, opt, nil)
		if !res.Passed {
			if res.Error != "" {
				return nil, errors.New(res.Error)
			}
			return nil, errors.New("@setup failed")
		}
		out := make(map[string]any, len(res.Defined))
		for name := range res.Defined {
			out[name] = res.Vars[name]
		}
		return out, nil
	}
	var got any
	var err error
	if once && e.opt.Suite != nil {
		got, err = e.opt.Suite.DoOnce(e.ctx, e.file.Path+"\x00setup\x00"+name, run)
	} else {
		got, err = run(e.ctx)
	}
	if err != nil {
		return nil, err
	}
	m, _ := got.(map[string]any)
	out := make(map[string]any, len(m))
	maps.Copy(out, m)
	return out, nil
}
