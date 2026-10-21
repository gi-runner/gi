package script

import (
	"errors"
	"maps"
	"strings"
	"time"

	"github.com/gi-runner/gi/internal/value"
	"github.com/grafana/sobek"
)

// Configuration keeps live callbacks within their owning scenario runtime.
// A cache keeps a sealed function, which still calls that runtime.
type Configuration struct {
	owner        *VM
	headers      any
	headerSource string
	hooks        map[string]sobek.Value
	hookFuncs    map[string]any
	hookSources  map[string]string
	retryN       int
	retryEvery   time.Duration
}

func (v *VM) Configuration() Configuration {
	return Configuration{owner: v, headers: v.headerValue, hooks: maps.Clone(v.hookValues), retryN: v.retryN, retryEvery: v.retryEvery}
}

// Portable copies data and sealed callbacks. A sealed callback keeps the runtime
// and lexical bindings it closed over. Source is only the fallback.
func (c Configuration) Portable() (Configuration, error) {
	out := Configuration{retryN: c.retryN, retryEvery: c.retryEvery, headerSource: c.headerSource, hookSources: maps.Clone(c.hookSources)}
	if out.hookSources == nil {
		out.hookSources = make(map[string]string)
	}
	switch header := c.headers.(type) {
	case jsFunc:
		if _, err := header.CloneValue(); err != nil {
			return Configuration{}, err
		}
		out.headers = header
		out.headerSource = ""
	case sobek.Value:
		if _, callable := sobek.AssertFunction(header); callable {
			out.headerSource = header.String()
		} else {
			out.headers = value.Normalize(header.Export())
		}
	default:
		out.headers = value.Normalize(header)
	}
	if strings.Contains(out.headerSource, "[native code]") {
		return Configuration{}, errors.New("native header callbacks cannot be cached")
	}
	if len(c.hookFuncs) > 0 {
		out.hookFuncs = make(map[string]any, len(c.hookFuncs))
		for name, fn := range c.hookFuncs {
			kept, ok := fn.(jsFunc)
			if !ok {
				return Configuration{}, errors.New("native hooks cannot be cached")
			}
			if _, err := kept.CloneValue(); err != nil {
				return Configuration{}, err
			}
			out.hookFuncs[name] = kept
		}
		return out, nil
	}
	for name, fn := range c.hooks {
		src := fn.String()
		if strings.Contains(src, "[native code]") {
			return Configuration{}, errors.New("native hooks cannot be cached")
		}
		out.hookSources[name] = src
	}
	return out, nil
}

func (v *VM) SealConfiguration(c Configuration) Configuration {
	if header, ok := c.headers.(sobek.Value); ok {
		if _, callable := sobek.AssertFunction(header); callable {
			c.headers = v.Seal(v.export(header))
		}
	}
	if len(c.hooks) == 0 {
		return c
	}
	c.hookFuncs = make(map[string]any, len(c.hooks))
	for name, fn := range c.hooks {
		if _, callable := sobek.AssertFunction(fn); callable {
			c.hookFuncs[name] = v.Seal(v.export(fn))
		}
	}
	c.hooks = nil
	return c
}

func (v *VM) ApplyConfiguration(c Configuration) error {
	if c.owner != nil && c.owner != v {
		return errors.New("configuration belongs to another JavaScript runtime")
	}
	var header sobek.Value
	var err error
	switch raw := c.headers.(type) {
	case jsFunc:
		header = v.toJS(raw)
	default:
		if c.headerSource != "" {
			header, err = v.run(c.headerSource)
			if err != nil {
				return err
			}
		} else if live, ok := c.headers.(sobek.Value); ok {
			header = live
		} else {
			header = v.rt.ToValue(c.headers)
		}
	}
	if err := v.storeHeaders(header); err != nil {
		return err
	}
	v.hooks = nil
	v.hookValues = nil
	for name, fn := range c.hooks {
		if err := v.storeHook(name, fn); err != nil {
			return err
		}
	}
	for name, fn := range c.hookFuncs {
		kept, ok := fn.(jsFunc)
		if !ok {
			return errors.New("native hooks cannot be cached")
		}
		if err := v.storeHook(name, v.toJS(kept)); err != nil {
			return err
		}
	}
	for name, src := range c.hookSources {
		if err := v.ConfigureHook(name, src); err != nil {
			return err
		}
	}
	v.retryN, v.retryEvery = c.retryN, c.retryEvery
	return nil
}

func (child *VM) adopt(parent *VM) error {
	child.retryN, child.retryEvery, child.retryUntil = parent.retryN, parent.retryEvery, parent.retryUntil
	if parent.headerFn != nil {
		if err := child.storeHeaders(child.toJS(jsFunc{vm: parent, v: parent.headerValue})); err != nil {
			return err
		}
	} else if parent.headerMap != nil {
		copied, err := value.Clone(parent.headerMap)
		if err != nil {
			return err
		}
		if err := child.storeHeaders(child.rt.ToValue(copied)); err != nil {
			return err
		}
	}
	for name, fn := range parent.hookValues {
		if err := child.storeHook(name, child.toJS(jsFunc{vm: parent, v: fn})); err != nil {
			return err
		}
	}
	return parent.copyBindings(child)
}

func (parent *VM) copyBindings(child *VM) error {
	for name := range parent.vars {
		if strings.HasPrefix(name, "__") {
			continue
		}
		rv := parent.rt.Get(name)
		if rv == nil || sobek.IsUndefined(rv) {
			if parent.vars[name] == nil {
				child.Set(name, nil)
			}
			continue
		}
		if sobek.IsNull(rv) {
			child.Set(name, nil)
			continue
		}
		if f, ok := parent.funcs[rv]; ok {
			child.Set(name, f)
			continue
		}
		if _, ok := sobek.AssertFunction(rv); ok {
			child.Set(name, jsFunc{vm: parent, v: rv})
			continue
		}
		if obj, ok := rv.(*sobek.Object); ok {
			if view, ok := obj.Export().(*xmlView); ok && view.doc != nil {
				copied, err := view.doc.CloneValue()
				if err != nil {
					return err
				}
				child.Set(name, copied)
				continue
			}
		}
		child.Set(name, parent.export(rv))
	}
	return nil
}

// SharedScope restores call arguments and helpers while preserving shared variables.
func (v *VM) SharedScope() func() {
	bindings := v.bindings
	global := v.rt.GlobalObject()
	names := []string{"__arg", "__loop", "__row", "__num"}
	saved := make(map[string]sobek.Value, len(names))
	for _, name := range names {
		saved[name] = global.Get(name)
	}
	return func() {
		for _, name := range names {
			if val := saved[name]; val != nil {
				_ = global.Set(name, val)
			} else {
				_ = global.Delete(name)
				delete(v.vars, name)
			}
		}
		_ = v.Bind(bindings)
	}
}

type FailureDecision struct {
	Override bool
	Continue bool
}

func (v *VM) StepFailure(info map[string]any) (FailureDecision, error) {
	fn := v.hooks["onStepFailure"]
	var decision FailureDecision
	if fn == nil {
		return decision, nil
	}
	obj := v.rt.ToValue(info).ToObject(v.rt)
	_ = obj.Set("proceed", func() { decision = FailureDecision{Override: true, Continue: true} })
	_ = obj.Set("stop", func() { decision = FailureDecision{Override: true, Continue: false} })
	_, err := fn(sobek.Undefined(), obj)
	return decision, err
}
