package script

import (
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/grafana/sobek"
)

// jsFunc stays on the runtime that created it.
// A closed function restores env around a later call.
type jsFunc struct {
	vm     *VM
	v      sobek.Value
	env    *functionEnv
	closed bool
}

func (f jsFunc) source() string {
	if f.v == nil {
		return ""
	}
	return f.v.String()
}

func (f jsFunc) CloneValue() (any, error) {
	src := f.source()
	if src == "" || strings.Contains(src, "[native code]") {
		return nil, errors.New("native functions cannot be cached")
	}
	return f, nil
}

type jsSource string

func (s jsSource) CloneValue() (any, error) { return s, nil }

func hasJSFunc(v any) bool {
	switch x := v.(type) {
	case jsFunc, jsSource:
		return true
	case map[string]any:
		for _, child := range x {
			if hasJSFunc(child) {
				return true
			}
		}
	case []any:
		return slices.ContainsFunc(x, hasJSFunc)
	}
	return false
}

func (v *VM) toJS(val any) sobek.Value {
	switch x := val.(type) {
	case jsFunc:
		if x.v == nil {
			return sobek.Undefined()
		}
		if !x.closed && x.vm == v {
			return x.v
		}
		wrapped := v.rt.ToValue(func(call sobek.FunctionCall) sobek.Value {
			return v.callCached(x, call.Arguments)
		})
		if v.funcs == nil {
			v.funcs = map[sobek.Value]jsFunc{}
		}
		v.funcs[wrapped] = x
		return wrapped
	case jsSource:
		return v.compileFn(string(x))
	case map[string]any:
		obj := v.rt.NewObject()
		for key, child := range x {
			_ = obj.Set(key, v.toJS(child))
		}
		return obj
	case []any:
		arr := v.rt.NewArray()
		for i, child := range x {
			_ = arr.Set(strconv.Itoa(i), v.toJS(child))
		}
		_ = arr.Set("length", len(x))
		return arr
	case host:
		return x.v
	default:
		return v.rt.ToValue(v.jsValue(val))
	}
}

// functionEnv belongs to its origin VM and is only accessed under that VM's
// execution lease. Functions returned together share mutable captured globals.
type functionEnv struct{ values map[string]sobek.Value }

func (v *VM) callCached(f jsFunc, args []sobek.Value) sobek.Value {
	origin := f.vm
	stop := func() {}
	if origin != v {
		var exported any
		var undefined bool
		var callErr error
		var panicked any
		release, ran, err := origin.handoff(v.ctx, func() {
			defer func() {
				if r := recover(); r != nil {
					panicked = r
				}
			}()
			exported, undefined, callErr = origin.evalCached(v, f, args)
		})
		if err != nil {
			panic(v.rt.NewGoError(err))
		}
		if ran {
			if panicked != nil {
				panic(panicked)
			}
			if callErr != nil {
				panic(v.rt.NewGoError(callErr))
			}
			if undefined {
				return sobek.Undefined()
			}
			if exported == nil {
				return sobek.Null()
			}
			return v.toJS(exported)
		}
		stop = origin.arm(v.ctx, release)
	}
	defer stop()
	restore := origin.install(f.env)
	defer restore()
	// Runtime helpers describe the current caller, not a completed scenario.
	bindings := origin.bindings
	if origin != v {
		if err := origin.Bind(v.bindings); err != nil {
			panic(v.rt.NewGoError(err))
		}
		defer func() { _ = origin.Bind(bindings) }()
	}
	fn, ok := sobek.AssertFunction(f.v)
	if !ok {
		panic(v.rt.NewGoError(errors.New("cached value is not a function")))
	}
	in := make([]sobek.Value, len(args))
	for i, arg := range args {
		switch {
		case sobek.IsUndefined(arg):
			in[i] = sobek.Undefined()
		case sobek.IsNull(arg):
			in[i] = sobek.Null()
		default:
			if origin == v {
				in[i] = arg
			} else {
				in[i] = origin.toJS(v.export(arg))
			}
		}
	}
	out, err := fn(sobek.Undefined(), in...)
	if err != nil {
		panic(v.rt.NewGoError(err))
	}
	if sobek.IsUndefined(out) {
		return sobek.Undefined()
	}
	if origin == v {
		return out
	}
	exported := origin.seal(origin.export(out), f.env)
	return v.toJS(exported)
}

func (origin *VM) evalCached(caller *VM, f jsFunc, args []sobek.Value) (any, bool, error) {
	if err := origin.ctx.Err(); err != nil {
		return nil, false, err
	}
	if err := caller.ctx.Err(); err != nil {
		return nil, false, err
	}
	// A handed-off call executes on the parked owner's goroutine but still
	// belongs to the caller's deadline. The outer owner retains its lease.
	stop := origin.arm(caller.ctx, func() {})
	defer stop()
	restore := origin.install(f.env)
	defer restore()
	bindings := origin.bindings
	if origin != caller {
		if err := origin.Bind(caller.bindings); err != nil {
			return nil, false, err
		}
		defer func() { _ = origin.Bind(bindings) }()
	}
	fn, ok := sobek.AssertFunction(f.v)
	if !ok {
		return nil, false, errors.New("cached value is not a function")
	}
	in := make([]sobek.Value, len(args))
	for i, arg := range args {
		switch {
		case sobek.IsUndefined(arg):
			in[i] = sobek.Undefined()
		case sobek.IsNull(arg):
			in[i] = sobek.Null()
		default:
			in[i] = origin.toJS(caller.export(arg))
		}
	}
	out, err := fn(sobek.Undefined(), in...)
	if err != nil {
		return nil, false, err
	}
	if sobek.IsUndefined(out) || sobek.IsNull(out) {
		return nil, sobek.IsUndefined(out), nil
	}
	return origin.seal(origin.export(out), f.env), false, nil
}

func (v *VM) install(env *functionEnv) func() {
	if env == nil {
		return func() {}
	}
	type saved struct {
		name string
		val  sobek.Value
		had  bool
	}
	prev := make([]saved, 0, len(env.values))
	for name, val := range env.values {
		cur := v.rt.Get(name)
		prev = append(prev, saved{name: name, val: cur, had: cur != nil})
		_ = v.rt.Set(name, val)
	}
	return func() {
		for _, item := range prev {
			// Commit scalar changes as well as object mutations before restoring scope.
			env.values[item.name] = v.rt.Get(item.name)
			if !item.had {
				_ = v.rt.GlobalObject().Delete(item.name)
			} else {
				_ = v.rt.Set(item.name, item.val)
			}
		}
	}
}

// Seal captures callable and null bindings along with data, retaining their
// native values so sibling functions and object identity survive scope restore.
func (v *VM) Seal(val any) any {
	env := &functionEnv{values: make(map[string]sobek.Value)}
	for name := range v.vars {
		if strings.HasPrefix(name, "__") || !jsIdent(name) {
			continue
		}
		if rv := v.rt.Get(name); rv != nil {
			env.values[name] = rv
		}
	}
	return v.seal(val, env)
}

func (v *VM) seal(val any, env *functionEnv) any {
	switch x := val.(type) {
	case jsFunc:
		if x.closed {
			return x
		}
		if x.vm == nil {
			x.vm = v
		}
		x.closed = true
		x.env = env
		return x
	case map[string]any:
		out := make(map[string]any, len(x))
		for key, child := range x {
			out[key] = v.seal(child, env)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, child := range x {
			out[i] = v.seal(child, env)
		}
		return out
	default:
		return val
	}
}

func jsIdent(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		if r == '_' || r == '$' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			continue
		}
		if i > 0 && r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}

func (v *VM) compileFn(src string) sobek.Value {
	got, err := v.run(src)
	if err != nil {
		panic(v.rt.NewGoError(err))
	}
	return got
}
