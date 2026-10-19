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

// Scope shares the runtime, preserving closures, while isolating data and
// configuration for assigned calls. Only top-level scenarios own a VM.
func (v *VM) Scope(shared bool) func() {
	bindings := v.bindings
	config := v.Configuration()
	vars := maps.Clone(v.vars)
	global := v.rt.GlobalObject()
	saved := make(map[string]sobek.Value)
	for _, name := range global.GetOwnPropertyNames() {
		saved[name] = global.Get(name)
	}
	prototypes := snapshotPrototypes(v.rt)
	if !shared {
		seen := make(map[*sobek.Object]*sobek.Object)
		for name := range vars {
			val := cloneJS(v.rt, global.Get(name), seen)
			_ = global.Set(name, val)
		}
	}
	return func() {
		if !shared {
			restorePrototypes(v.rt, prototypes)
			for _, name := range global.GetOwnPropertyNames() {
				if _, exists := saved[name]; !exists {
					_ = global.Delete(name)
				}
			}
			for name, val := range saved {
				_ = global.Set(name, val)
			}
			v.vars = vars
			// Live values belong to this VM and are already validated.
			if err := v.ApplyConfiguration(config); err != nil {
				panic(err)
			}
		} else {
			for _, name := range []string{"__arg", "__loop", "__row", "__num"} {
				if val, exists := saved[name]; exists {
					_ = global.Set(name, val)
				} else {
					_ = global.Delete(name)
					delete(v.vars, name)
				}
			}
		}
		_ = v.Bind(bindings)
	}
}

func snapshotPrototypes(rt *sobek.Runtime) map[string]map[string]sobek.Value {
	out := make(map[string]map[string]sobek.Value)
	for _, name := range []string{"Object", "Array", "String"} {
		obj := prototype(rt, name)
		if obj == nil {
			continue
		}
		saved := make(map[string]sobek.Value)
		for _, key := range obj.Keys() {
			saved[key] = obj.Get(key)
		}
		out[name] = saved
	}
	return out
}

func restorePrototypes(rt *sobek.Runtime, saved map[string]map[string]sobek.Value) {
	for name, props := range saved {
		proto := prototype(rt, name)
		if proto == nil {
			continue
		}
		for _, key := range proto.Keys() {
			if _, ok := props[key]; !ok {
				_ = proto.Delete(key)
			}
		}
		for key, val := range props {
			_ = proto.Set(key, val)
		}
	}
}

func prototype(rt *sobek.Runtime, name string) *sobek.Object {
	ctor := rt.Get(name)
	if ctor == nil || sobek.IsUndefined(ctor) {
		return nil
	}
	proto := ctor.ToObject(rt).Get("prototype")
	if proto == nil || sobek.IsUndefined(proto) {
		return nil
	}
	return proto.ToObject(rt)
}

func cloneJS(rt *sobek.Runtime, val sobek.Value, seen map[*sobek.Object]*sobek.Object) sobek.Value {
	obj, ok := val.(*sobek.Object)
	if !ok {
		return val
	}
	if view, ok := obj.Export().(*xmlView); ok && view.doc != nil {
		copied, err := view.doc.CloneValue()
		if err != nil {
			panic(err)
		}
		return view.vm.xmlReadValue(copied)
	}
	if _, fn := sobek.AssertFunction(val); fn {
		return val
	}
	if flag := obj.Get("__gi"); flag != nil && flag.ToBoolean() {
		return val
	}
	if copied := seen[obj]; copied != nil {
		return copied
	}
	var copied *sobek.Object
	switch obj.ClassName() {
	case "Array":
		copied = rt.NewArray()
	case "Object":
		copied = rt.NewObject()
	default:
		return val
	}
	seen[obj] = copied
	for _, key := range obj.Keys() {
		_ = copied.Set(key, cloneJS(rt, obj.Get(key), seen))
	}
	if obj.ClassName() == "Array" {
		_ = copied.Set("length", obj.Get("length"))
	}
	return copied
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
