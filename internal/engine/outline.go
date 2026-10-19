package engine

import (
	"context"
	"errors"
	"fmt"

	"github.com/gi-runner/gi/internal/feature"
	"github.com/gi-runner/gi/internal/value"
)

func expand(ctx context.Context, f *feature.File, opt Options, include func(feature.Scenario) (bool, error)) (*feature.File, error) {
	var scenarios []feature.Scenario
	pending := false
	for _, sc := range f.Scenarios {
		if sc.Outline != "" {
			pending = true
			break
		}
	}
	if !pending {
		return f, nil
	}
	for _, sc := range f.Scenarios {
		selected, err := include(sc)
		if err != nil {
			return nil, err
		}
		if sc.Outline == "" || !selected {
			scenarios = append(scenarios, sc)
			continue
		}
		rows, err := outlineRows(ctx, f, sc.Outline, opt)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", sc.Name, err)
		}
		expanded, err := feature.Materialize(sc, rows)
		if err != nil {
			return nil, err
		}
		scenarios = append(scenarios, expanded...)
	}
	out := *f
	out.Scenarios = scenarios
	return &out, nil
}

func outlineRows(ctx context.Context, f *feature.File, expr string, opt Options) (rows []map[string]any, err error) {
	ex := newExec(ctx, f, opt)
	stop := func() {}
	if opt.vm == nil {
		stop, err = ex.vm.Watch(ctx)
		if err != nil {
			return nil, err
		}
	}
	defer func() {
		defer stop()
		if cerr := ex.release(); cerr != nil && err == nil {
			err = cerr
		}
	}()
	if err := ex.bind(); err != nil {
		return nil, err
	}
	if err := ex.evalSuite(); err != nil {
		return nil, err
	}
	if opt.vm == nil {
		for k, v := range opt.Vars {
			ex.vm.Set(k, v)
		}
	}
	got, err := ex.eval(expr)
	if err != nil {
		return nil, err
	}
	items, ok := value.AsSlice(got)
	if !ok {
		return nil, errors.New("examples expression must return a list")
	}
	rows = make([]map[string]any, 0, len(items))
	for _, item := range items {
		m, ok := value.AsMap(item)
		if !ok {
			return nil, errors.New("examples row must be an object")
		}
		rows = append(rows, m)
	}
	return rows, nil
}
