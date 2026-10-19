package script

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gi-runner/gi/internal/suite"
	"github.com/gi-runner/gi/internal/value"
	"github.com/gi-runner/gi/internal/xml"
	"github.com/grafana/sobek"
	"gopkg.in/yaml.v3"
)

// Call describes a feature or JavaScript file invocation.
type Call struct {
	Path   string
	Arg    any
	Once   bool
	Shared bool
}

type Bindings struct {
	Driver     func(string, []any) (any, error)
	Call       func(Call) (any, error)
	Env        string
	Root       string
	FeatureDir string
	Configure  func(key string, val any) error
	CallSingle func(path string, arg any) (any, error)
	Setup      func(name string, once bool) (any, error)
	Channel    func(kind string, spec map[string]any) (any, error)
	PageText   func(selector string) (string, error)
	Abort      func()
	Sleep      func(time.Duration, <-chan struct{}) error
	Log        func(string) error
	Info       func() map[string]any
	Scenario   func() map[string]any
	Feature    func() map[string]any
	Write      func(any, string) (string, error)
	WaitHTTP   func(string, time.Duration, time.Duration, <-chan struct{}) error
	WaitPort   func(string, int, time.Duration, time.Duration, <-chan struct{}) error
	Signal     func(any) error
	Listen     func(time.Duration, <-chan struct{}) (any, error)
	Embed      func(any, string, string) error
	Helpers    map[string]func([]any) (any, error)
}

func (v *VM) Bind(b Bindings) {
	v.bindings = b
	v.root = b.Root
	v.featureDir = b.FeatureDir
	if v.karate != nil {
		_ = v.karate.Set("env", b.Env)
		v.bindRuntime()
		return
	}
	read := func(call sobek.FunctionCall) sobek.Value {
		val, err := v.read(call.Argument(0).String())
		if err != nil {
			panic(v.rt.NewGoError(err))
		}
		return val
	}
	obj := v.rt.NewObject()
	v.karate = obj
	_ = obj.Set("env", b.Env)
	_ = obj.Set("read", read)
	_ = obj.Set("configure", func(call sobek.FunctionCall) sobek.Value {
		if err := v.configure(call.Argument(0).String(), call.Argument(1), v.bindings.Configure); err != nil {
			panic(v.rt.NewGoError(err))
		}
		return sobek.Undefined()
	})
	_ = obj.Set("callSingle", func(call sobek.FunctionCall) sobek.Value {
		if v.bindings.CallSingle == nil {
			panic(v.rt.NewGoError(errors.New("callSingle is not available")))
		}
		var arg any
		if len(call.Arguments) > 1 {
			arg = value.Normalize(call.Argument(1).Export())
		}
		got, err := v.bindings.CallSingle(call.Argument(0).String(), arg)
		if err != nil {
			panic(v.rt.NewGoError(err))
		}
		return v.ToValue(got)
	})
	_ = obj.Set("setup", v.invokeSetup(func(name string, once bool) (any, error) {
		if v.bindings.Setup == nil {
			return nil, errors.New("setup is not available")
		}
		return v.bindings.Setup(name, once)
	}, false))
	_ = obj.Set("setupOnce", v.invokeSetup(func(name string, once bool) (any, error) {
		if v.bindings.Setup == nil {
			return nil, errors.New("setup is not available")
		}
		return v.bindings.Setup(name, once)
	}, true))
	_ = obj.Set("channel", func(call sobek.FunctionCall) sobek.Value {
		if v.bindings.Channel == nil {
			panic(v.rt.NewGoError(errors.New("channel is not available")))
		}
		spec := map[string]any{}
		if len(call.Arguments) > 1 && !sobek.IsUndefined(call.Argument(1)) && !sobek.IsNull(call.Argument(1)) {
			if m, ok := value.Normalize(call.Argument(1).Export()).(map[string]any); ok {
				spec = m
			}
		}
		got, err := v.bindings.Channel(call.Argument(0).String(), spec)
		if err != nil {
			panic(v.rt.NewGoError(err))
		}
		if sv, ok := got.(sobek.Value); ok {
			return sv
		}
		return v.ToValue(got)
	})
	_ = obj.Set("pageText", func(call sobek.FunctionCall) sobek.Value {
		if v.bindings.PageText == nil {
			panic(v.rt.NewGoError(errors.New("browser is not set")))
		}
		got, err := v.bindings.PageText(call.Argument(0).String())
		if err != nil {
			panic(v.rt.NewGoError(err))
		}
		return v.ToValue(got)
	})
	for _, name := range []string{"call", "callonce"} {
		once := name == "callonce"
		_ = obj.Set(name, func(call sobek.FunctionCall) sobek.Value {
			if v.bindings.Call == nil {
				panic(v.rt.NewGoError(errors.New("call is not available")))
			}
			index := 0
			shared := false
			if flag, ok := call.Argument(0).Export().(bool); ok {
				shared = flag
				index = 1
			}
			if len(call.Arguments) <= index || sobek.IsUndefined(call.Argument(index)) {
				panic(v.rt.NewGoError(errors.New("call requires a path")))
			}
			var arg any
			if len(call.Arguments) > index+1 {
				arg = value.Normalize(call.Argument(index + 1).Export())
			}
			got, err := v.bindings.Call(Call{Path: call.Argument(index).String(), Arg: arg, Once: once, Shared: shared})
			if err != nil {
				panic(v.rt.NewGoError(err))
			}
			return v.ToValue(got)
		})
	}
	_ = v.rt.Set("gi", obj)
	_ = v.rt.Set("karate", obj)
	v.bindHelpers(obj)
	v.bindDriver()
	v.bindRuntime()
	_ = v.rt.Set("read", read)
}

func (v *VM) EvalConfig(src string) error {
	src = strings.TrimSpace(src)
	if src == "" {
		return nil
	}
	val, err := v.run(src)
	if err != nil {
		return err
	}

	return v.applyConfig(val)
}

func (v *VM) applyConfig(val sobek.Value) error {
	if val == nil || sobek.IsUndefined(val) || sobek.IsNull(val) {
		return nil
	}
	if fn, ok := sobek.AssertFunction(val); ok {
		var err error
		val, err = fn(sobek.Undefined())
		if err != nil {
			return err
		}
	}
	if val == nil || sobek.IsUndefined(val) || sobek.IsNull(val) {
		return nil
	}
	m, ok := value.Normalize(val.Export()).(map[string]any)
	if !ok {
		return errors.New("config did not return an object")
	}
	for k, x := range m {
		v.Set(k, x)
	}
	return nil
}

func (v *VM) configure(key string, arg sobek.Value, other func(string, any) error) error {
	switch key {
	case "headers":
		return v.storeHeaders(arg)
	case "retry":
		return v.ConfigureRetry(value.Normalize(arg.Export()))
	case "beforeScenario", "afterScenario", "afterFeature", "afterScenarioOutline", "onStepFailure":
		return v.storeHook(key, arg)
	default:
		if other == nil {
			return fmt.Errorf("unknown configure %q", key)
		}
		return other(key, value.Normalize(arg.Export()))
	}
}

func (v *VM) ConfigureHeaders(expr string) error {
	val, err := v.run(rewriteEmbeds(expr))
	if err != nil {
		return err
	}
	return v.storeHeaders(val)
}

func (v *VM) storeHeaders(val sobek.Value) error {
	v.headerValue = val
	if val == nil || sobek.IsUndefined(val) || sobek.IsNull(val) {
		v.headerFn = nil
		v.headerMap = nil
		return nil
	}
	if fn, ok := sobek.AssertFunction(val); ok {
		v.headerFn = fn
		v.headerMap = nil
		return nil
	}
	m, ok := value.Normalize(val.Export()).(map[string]any)
	if !ok {
		return errors.New("headers expects an object or a function")
	}
	v.headerFn = nil
	v.headerMap = m
	return nil
}

func (v *VM) HeaderOverlay() (map[string]any, error) {
	if v.headerFn != nil {
		out, err := v.headerFn(sobek.Undefined())
		if err != nil {
			return nil, err
		}
		if out == nil || sobek.IsUndefined(out) || sobek.IsNull(out) {
			return map[string]any{}, nil
		}
		return headerObject(value.Normalize(out.Export()))
	}
	return headerObject(v.headerMap)
}

func headerObject(v any) (map[string]any, error) {
	if v == nil {
		return map[string]any{}, nil
	}
	m, ok := value.AsMap(v)
	if !ok {
		return nil, errors.New("headers function must return an object")
	}
	return m, nil
}

func (v *VM) Bridge(methods map[string]func([]any) (any, error)) sobek.Value {
	obj := v.rt.NewObject()
	_ = obj.Set("__gi", true)
	for name, fn := range methods {
		_ = obj.Set(name, func(call sobek.FunctionCall) sobek.Value {
			args := make([]any, len(call.Arguments))
			for i, a := range call.Arguments {
				if sobek.IsUndefined(a) || sobek.IsNull(a) {
					continue
				}
				args[i] = value.Normalize(a.Export())
			}
			got, err := fn(args)
			if err != nil {
				panic(v.rt.NewGoError(err))
			}
			if got == nil {
				return sobek.Undefined()
			}
			return v.ToValue(got)
		})
	}
	return obj
}

func (v *VM) invokeSetup(fn func(string, bool) (any, error), once bool) func(sobek.FunctionCall) sobek.Value {
	return func(call sobek.FunctionCall) sobek.Value {
		if fn == nil {
			panic(v.rt.NewGoError(errors.New("setup is not available")))
		}
		name := ""
		if len(call.Arguments) > 0 && !sobek.IsUndefined(call.Argument(0)) && !sobek.IsNull(call.Argument(0)) {
			name = call.Argument(0).String()
		}
		got, err := fn(name, once)
		if err != nil {
			panic(v.rt.NewGoError(err))
		}
		return v.ToValue(got)
	}
}

func (v *VM) ConfigureHook(name, expr string) error {
	val, err := v.run(rewriteEmbeds(expr))
	if err != nil {
		return err
	}
	return v.storeHook(name, val)
}

func (v *VM) storeHook(name string, val sobek.Value) error {
	if val == nil || sobek.IsUndefined(val) || sobek.IsNull(val) {
		delete(v.hooks, name)
		delete(v.hookValues, name)
		return nil
	}
	fn, ok := sobek.AssertFunction(val)
	if !ok {
		return fmt.Errorf("%s expects a function", name)
	}
	if v.hooks == nil {
		v.hooks = map[string]sobek.Callable{}
	}
	v.hooks[name] = fn
	if v.hookValues == nil {
		v.hookValues = make(map[string]sobek.Value)
	}
	v.hookValues[name] = val
	return nil
}

func (v *VM) CallHook(name string) (bool, error) {
	fn := v.hooks[name]
	if fn == nil {
		return false, nil
	}
	_, err := fn(sobek.Undefined())
	return true, err
}

func (v *VM) ConfigureRetry(val any) error {
	m, ok := value.AsMap(val)
	if !ok {
		return errors.New("retry expects an object")
	}
	count, interval := v.retryN, v.retryEvery
	if raw, exists := m["count"]; exists {
		n, ok := value.AsInt(raw)
		if !ok || n <= 0 {
			return errors.New("retry count expects a positive integer")
		}
		count = n
	}
	if raw, exists := m["interval"]; exists {
		n, ok := value.AsInt(raw)
		if !ok || n < 0 || int64(n) > math.MaxInt64/int64(time.Millisecond) {
			return errors.New("retry interval expects nonnegative milliseconds within the duration range")
		}
		interval = time.Duration(n) * time.Millisecond
	}
	v.retryN, v.retryEvery = count, interval
	return nil
}

func (v *VM) SetRetryUntil(expr string) { v.retryUntil = expr }

func (v *VM) TakeRetryUntil() (expr string, count int, every time.Duration) {
	expr = v.retryUntil
	v.retryUntil = ""
	count = v.retryN
	if expr != "" && count < 1 {
		count = 3
	}
	if expr == "" {
		count = 1
	}
	return expr, count, v.retryEvery
}

func (v *VM) Bytes(ref string) ([]byte, error) {
	return os.ReadFile(suite.Resolve(v.root, v.featureDir, ref))
}

func (v *VM) read(ref string) (sobek.Value, error) {
	target, selector, hasSelector := strings.Cut(ref, "@")
	suffix := ""
	if i := strings.Index(target, ".feature:"); i >= 0 {
		suffix = target[i+len(".feature"):]
		target = target[:i+len(".feature")]
	}
	if filepath.Ext(target) == ".feature" {
		path := suite.Resolve(v.root, v.featureDir, target)
		if _, err := os.Stat(path); err != nil {
			return nil, err
		}
		absolute, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		result := "file:" + absolute + suffix
		if hasSelector {
			result += "@" + selector
		}
		return v.rt.ToValue(result), nil
	}
	path := suite.Resolve(v.root, v.featureDir, ref)
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		val, err := value.Parse(b)
		if err != nil {
			return nil, err
		}
		val, err = v.embeds(val)
		if err != nil {
			return nil, err
		}
		return v.ToValue(val), nil
	case ".js":
		return v.run(string(b))
	case ".yaml", ".yml":
		var val any
		if err := yaml.Unmarshal(b, &val); err != nil {
			return nil, err
		}
		return v.rt.ToValue(value.Normalize(val)), nil
	case ".csv":
		val, err := value.ParseCSV(string(b))
		if err != nil {
			return nil, err
		}
		return v.ToValue(val), nil
	case ".xml":
		val, err := xml.ParseDocument(string(b))
		if err != nil {
			return nil, err
		}
		return v.ToValue(val), nil
	case ".png", ".jpg", ".jpeg", ".gif", ".pdf", ".zip", ".bin", ".gz":
		return v.rt.ToValue(b), nil
	default:
		return v.rt.ToValue(string(b)), nil
	}
}

func (v *VM) embeds(x any) (any, error) {
	switch x := x.(type) {
	case string:
		optional := strings.HasPrefix(x, "##(")
		expr := x
		if optional {
			expr = x[1:]
		}
		if strings.HasPrefix(expr, "#(") && strings.HasSuffix(expr, ")") {
			got, err := v.Eval(expr[2 : len(expr)-1])
			if err != nil {
				return nil, err
			}
			if optional && got == nil {
				return value.Absent{}, nil
			}
			return got, nil
		}
		return x, nil
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, child := range x {
			got, err := v.embeds(child)
			if err != nil {
				return nil, err
			}
			if !value.IsAbsent(got) {
				out[k] = got
			}
		}
		return out, nil
	case []any:
		out := make([]any, 0, len(x))
		for _, child := range x {
			got, err := v.embeds(child)
			if err != nil {
				return nil, err
			}
			if !value.IsAbsent(got) {
				out = append(out, got)
			}
		}
		return out, nil
	default:
		return x, nil
	}
}
