package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/gi-runner/gi/internal/value"
	"github.com/gi-runner/gi/internal/xml"
)

func (e *exec) release() error {
	var errs []error
	for _, link := range e.links {
		if link.Close != nil {
			if err := link.Close(); err != nil {
				errs = append(errs, err)
			}
		}
	}
	e.links = nil
	if e.page != nil && !e.pageInherited {
		if err := e.page.Close(); err != nil {
			errs = append(errs, err)
		}
		e.page = nil
	}
	return errors.Join(errs...)
}

func (e *exec) op(ctx context.Context) (context.Context, context.CancelFunc) {
	if e.sess.timeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, e.sess.timeout)
}

func (e *exec) openChannel(kind string, spec map[string]any) (any, error) {
	if e.opt.Dial == nil {
		return nil, fmt.Errorf("%s is not available", kind)
	}
	ctx, cancel := e.op(e.ctx)
	link, err := e.opt.Dial(ctx, kind, spec)
	cancel()
	if err != nil {
		return nil, err
	}
	if link.Close != nil {
		closeLink := link.Close
		var once sync.Once
		var closeErr error
		link.Close = func() error { once.Do(func() { closeErr = closeLink() }); return closeErr }
	}
	e.links = append(e.links, link)
	return e.vm.Bridge(map[string]func([]any) (any, error){
		"send": func(args []any) (any, error) {
			if link.Send == nil {
				return nil, fmt.Errorf("%s does not send", kind)
			}
			var v any
			if len(args) > 0 {
				v = args[0]
			}
			ctx, cancel := e.op(e.ctx)
			defer cancel()
			return nil, link.Send(ctx, v)
		},
		"receive": func([]any) (any, error) {
			if link.Receive == nil {
				return nil, fmt.Errorf("%s does not receive", kind)
			}
			ctx, cancel := e.op(e.ctx)
			defer cancel()
			return link.Receive(ctx)
		},
		"call": func(args []any) (any, error) {
			if link.Call == nil {
				return nil, fmt.Errorf("%s does not call", kind)
			}
			if len(args) < 1 {
				return nil, errors.New("call requires a method")
			}
			var body any
			if len(args) > 1 {
				body = args[1]
			}
			ctx, cancel := e.op(e.ctx)
			defer cancel()
			return link.Call(ctx, value.Sprint(args[0]), body)
		},
		"open": func(args []any) (any, error) {
			if link.Open == nil {
				return nil, fmt.Errorf("%s does not open streams", kind)
			}
			if len(args) < 1 {
				return nil, errors.New("open requires a method")
			}
			st, err := e.openStream(link, value.Sprint(args[0]))
			if err != nil {
				return nil, err
			}
			return e.vm.Bridge(map[string]func([]any) (any, error){
				"send": func(args []any) (any, error) {
					if st.Send == nil {
						return nil, errors.New("stream does not send")
					}
					var v any
					if len(args) > 0 {
						v = args[0]
					}
					ctx, cancel := e.op(e.ctx)
					defer cancel()
					return nil, st.Send(ctx, v)
				},
				"receive": func([]any) (any, error) {
					if st.Receive == nil {
						return nil, errors.New("stream does not receive")
					}
					ctx, cancel := e.op(e.ctx)
					defer cancel()
					return st.Receive(ctx)
				},
				"closeSend": func([]any) (any, error) {
					if st.CloseSend == nil {
						return true, nil
					}
					return true, st.CloseSend()
				},
				"close": func([]any) (any, error) {
					if st.Close == nil {
						return true, nil
					}
					return true, st.Close()
				},
			}), nil
		},
		"close": func([]any) (any, error) {
			if link.Close == nil {
				return true, nil
			}
			return true, link.Close()
		},
	}), nil
}

func (e *exec) xmlStep(rest, doc string) error {
	name, rhs, ok := splitAssign(rest)
	if !ok {
		return errors.New("xml expects name = value")
	}
	if strings.TrimSpace(rhs) == "" {
		rhs = doc
	}
	v, err := e.eval(rhs)
	if err != nil {
		return err
	}
	switch x := v.(type) {
	case *xml.Document:
		return e.assign(name, x)
	case string:
		parsed, err := xml.ParseDocument(x)
		if err != nil {
			return fmt.Errorf("xml: %w", err)
		}
		return e.assign(name, parsed)
	default:
		m, ok := value.AsMap(v)
		if !ok {
			return errors.New("xml expects a document, a string, or an object")
		}
		parsed, err := xml.FromMap(m)
		if err != nil {
			return fmt.Errorf("xml: %w", err)
		}
		return e.assign(name, parsed)
	}
}

func (e *exec) xmlStringStep(rest, doc string) error {
	name, rhs, ok := splitAssign(rest)
	if !ok {
		return errors.New("xmlstring expects name = value")
	}
	if strings.TrimSpace(rhs) == "" {
		rhs = doc
	}
	v, err := e.eval(rhs)
	if err != nil {
		return err
	}
	if s, ok := v.(string); ok {
		return e.assign(name, s)
	}
	out, err := xml.Format(v)
	if err != nil {
		return err
	}
	return e.assign(name, out)
}

func (e *exec) driverStep(ctx context.Context, expr string) error {
	v, err := e.eval(expr)
	if err != nil {
		return err
	}
	raw := value.Sprint(v)
	if raw == "" {
		return errors.New("driver expects a url")
	}
	if e.page == nil {
		if e.opt.NewPage == nil {
			return errors.New("browser is not set")
		}
		page, err := e.opt.NewPage(ctx)
		if err != nil {
			return err
		}
		e.page = page
	}
	ctx, cancel := e.op(ctx)
	defer cancel()
	return e.page.Open(ctx, raw)
}

func (e *exec) clickStep(ctx context.Context, expr string) error {
	if err := e.needPage(); err != nil {
		return err
	}
	v, err := e.eval(expr)
	if err != nil {
		return err
	}
	ctx, cancel := e.op(ctx)
	defer cancel()
	return e.page.Click(ctx, value.Sprint(v))
}

func (e *exec) inputStep(ctx context.Context, rest string) error {
	if err := e.needPage(); err != nil {
		return err
	}
	sel, expr, ok := splitAssign(rest)
	if !ok {
		return errors.New("input expects selector = value")
	}
	s, err := e.eval(sel)
	if err != nil {
		return err
	}
	v, err := e.eval(expr)
	if err != nil {
		return err
	}
	ctx, cancel := e.op(ctx)
	defer cancel()
	return e.page.Fill(ctx, value.Sprint(s), value.Sprint(v))
}

func (e *exec) pageText(sel string) (string, error) {
	ctx := e.ctx
	if err := e.needPage(); err != nil {
		return "", err
	}
	ctx, cancel := e.op(ctx)
	defer cancel()
	return e.page.Text(ctx, sel)
}

func (e *exec) needPage() error {
	if e.page == nil {
		return errors.New("driver is not open")
	}
	return nil
}

// Opening has an operation timeout; an established stream lives until closed or
// the scenario ends. Cancelling the opening timer must not cancel that stream.
func (e *exec) openStream(link Link, method string) (Stream, error) {
	opening, finish := e.op(e.ctx)
	defer finish()
	lifetime, cancel := context.WithCancel(e.ctx)
	stop := context.AfterFunc(opening, cancel)
	st, err := link.Open(lifetime, method)
	stop()
	if opening.Err() != nil {
		err = opening.Err()
	}
	if err != nil {
		cancel()
		if st.Close != nil {
			_ = st.Close()
		}
		return Stream{}, err
	}
	closeStream := st.Close
	st.Close = func() error {
		cancel()
		if closeStream != nil {
			return closeStream()
		}
		return nil
	}
	return st, nil
}
