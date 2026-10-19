package engine

import (
	"context"
	"errors"
	"fmt"

	"github.com/gi-runner/gi/internal/script"
	"github.com/gi-runner/gi/internal/value"
)

// Advanced actions are optional, keeping the core Page seam small for embedders
// and tests that only need navigation, click, input, and text.
type pageActions interface {
	Action(context.Context, string, []any) (any, error)
}

func (e *exec) driverCall(name string, args []any) (any, error) {
	text := func(i int) string {
		if i >= len(args) {
			return ""
		}
		return value.Sprint(args[i])
	}
	if name == "open" {
		if len(args) != 1 {
			return nil, errors.New("driver.url requires a URL")
		}
		if e.page == nil {
			if e.opt.NewPage == nil {
				return nil, errors.New("browser is not available")
			}
			page, err := e.opt.NewPage(e.ctx)
			if err != nil {
				return nil, err
			}
			e.page = page
			e.pageInherited = false
		}
		ctx, cancel := e.op(e.ctx)
		defer cancel()
		return nil, e.page.Open(ctx, text(0))
	}
	if err := e.needPage(); err != nil {
		return nil, err
	}
	if name == "quit" {
		err := e.page.Close()
		e.page = nil
		return nil, err
	}
	ctx, cancel := e.op(e.ctx)
	defer cancel()
	var got any
	var err error
	if advanced, ok := e.page.(pageActions); ok {
		got, err = advanced.Action(ctx, name, args)
	} else {
		switch name {
		case "click":
			err = e.page.Click(ctx, text(0))
		case "input":
			if len(args) < 2 {
				return nil, errors.New("input requires a locator and text")
			}
			err = e.page.Fill(ctx, text(0), text(1))
		case "text":
			got, err = e.page.Text(ctx, text(0))
		default:
			return nil, fmt.Errorf("driver action %q is not supported by this page", name)
		}
	}
	if err != nil {
		return nil, err
	}
	switch name {
	case "click", "input", "clear", "select", "focus", "scroll", "locate", "waitFor", "waitForText", "waitForEnabled":
		return script.Element{Locator: text(0)}, nil
	case "locateAll":
		items, ok := value.AsSlice(value.Normalize(got))
		if !ok {
			return nil, errors.New("driver returned invalid locator list")
		}
		out := make([]script.Element, len(items))
		for i, item := range items {
			out[i] = script.Element{Locator: value.Sprint(item)}
		}
		return out, nil
	case "screenshot":
		if raw, ok := got.([]byte); ok {
			if err := e.embed(raw, "image/png", "screenshot"); err != nil {
				return nil, err
			}
		}
		return got, nil
	default:
		return got, nil
	}
}
