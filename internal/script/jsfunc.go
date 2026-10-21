package script

import (
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/grafana/sobek"
	"github.com/grafana/sobek/parser"
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
	// The caller's goroutine already owns this runtime, so taking its gate would wait on itself.
	if origin != v && !leased(v.ctx, origin) {
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
	} else if origin != v {
		// Re-entry keeps the lease but must still observe this caller's deadline.
		stop = origin.arm(v.ctx, func() {})
	}
	defer stop()
	out, err := origin.invokeCached(v, f, args)
	if err != nil {
		panic(v.rt.NewGoError(err))
	}
	if origin == v || sobek.IsUndefined(out) {
		return out
	}
	return v.toJS(origin.seal(origin.export(out), f.env))
}

func (origin *VM) evalCached(caller *VM, f jsFunc, args []sobek.Value) (any, bool, error) {
	if err := origin.ctx.Err(); err != nil {
		return nil, false, err
	}
	if err := caller.ctx.Err(); err != nil {
		return nil, false, err
	}
	// A handed-off call retains the parked owner's lease and uses the caller's deadline.
	stop := origin.arm(caller.ctx, func() {})
	defer stop()
	out, err := origin.invokeCached(caller, f, args)
	if err != nil {
		return nil, false, err
	}
	if sobek.IsUndefined(out) || sobek.IsNull(out) {
		return nil, sobek.IsUndefined(out), nil
	}
	return origin.seal(origin.export(out), f.env), false, nil
}

// invokeCached is the single invocation path after runtime ownership is established.
func (origin *VM) invokeCached(caller *VM, f jsFunc, args []sobek.Value) (out sobek.Value, err error) {
	restore := origin.install(f.env)
	defer restore()
	if origin != caller {
		bindings := origin.bindings
		if err := origin.Bind(caller.bindings); err != nil {
			return nil, err
		}
		defer func() {
			if restoreErr := origin.Bind(bindings); restoreErr != nil {
				err = errors.Join(err, restoreErr)
			}
		}()
	}
	fn, ok := sobek.AssertFunction(f.v)
	if !ok {
		return nil, errors.New("cached value is not a function")
	}
	in := make([]sobek.Value, len(args))
	for i, arg := range args {
		if origin == caller || sobek.IsUndefined(arg) || sobek.IsNull(arg) {
			in[i] = arg
		} else {
			in[i] = origin.toJS(caller.export(arg))
		}
	}
	return fn(sobek.Undefined(), in...)
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
		if strings.HasPrefix(name, "__") || !parser.IsIdentifier(name) {
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

func (v *VM) compileFn(src string) sobek.Value {
	got, err := v.run(src)
	if err != nil {
		panic(v.rt.NewGoError(err))
	}
	return got
}
