package script

import (
	"errors"
	"strings"
	"time"

	"github.com/gi-runner/gi/internal/suite"
	"github.com/gi-runner/gi/internal/value"
	"github.com/grafana/sobek"
)

func (v *VM) bindRuntime() {
	obj := v.karate
	_ = obj.Set("abort", func(sobek.FunctionCall) sobek.Value {
		if v.bindings.Abort != nil {
			v.bindings.Abort()
		}
		return sobek.Undefined()
	})
	_ = obj.Set("pause", func(call sobek.FunctionCall) sobek.Value {
		if v.bindings.Sleep == nil || len(call.Arguments) == 0 || missing(call.Argument(0)) {
			return sobek.Undefined()
		}
		wait := time.Duration(call.Argument(0).ToFloat()) * time.Millisecond
		if wait < 0 {
			if err := v.bindings.Sleep(wait, nil); err != nil {
				panic(v.rt.NewGoError(err))
			}
			return sobek.Undefined()
		}
		deadline := time.Now().Add(wait)
		err := v.Park(func(wake <-chan struct{}) (bool, error) {
			remaining := time.Until(deadline)
			if remaining <= 0 {
				return false, nil
			}
			sleepErr := v.bindings.Sleep(remaining, wake)
			if errors.Is(sleepErr, suite.ErrYield) {
				return true, nil
			}
			return false, sleepErr
		})
		if err != nil {
			panic(v.rt.NewGoError(err))
		}
		return sobek.Undefined()
	})
	_ = obj.Set("log", func(call sobek.FunctionCall) sobek.Value {
		if v.bindings.Log == nil {
			return sobek.Undefined()
		}
		parts := make([]string, len(call.Arguments))
		for i, arg := range call.Arguments {
			if missing(arg) {
				parts[i] = "null"
				continue
			}
			parts[i] = value.Format(value.Normalize(arg.Export()))
		}
		if err := v.bindings.Log(join(parts)); err != nil {
			panic(v.rt.NewGoError(err))
		}
		return sobek.Undefined()
	})
	_ = obj.Set("write", func(call sobek.FunctionCall) sobek.Value {
		if v.bindings.Write == nil || len(call.Arguments) < 2 {
			panic(v.rt.NewGoError(errors.New("write requires a value and a path")))
		}
		var val any
		if !missing(call.Argument(0)) {
			val = value.Normalize(call.Argument(0).Export())
		}
		path, err := v.bindings.Write(val, call.Argument(1).String())
		if err != nil {
			panic(v.rt.NewGoError(err))
		}
		return v.rt.ToValue(path)
	})
	_ = obj.Set("waitForHttp", func(call sobek.FunctionCall) sobek.Value {
		if v.bindings.WaitHTTP == nil || len(call.Arguments) == 0 || missing(call.Argument(0)) {
			panic(v.rt.NewGoError(errors.New("waitForHttp requires a url")))
		}
		timeout := fieldDuration(call.Argument(1), "timeout", 30*time.Second)
		every := fieldDuration(call.Argument(1), "interval", 250*time.Millisecond)
		raw := call.Argument(0).String()
		if err := v.parkWait(timeout, func(remaining time.Duration, wake <-chan struct{}) error {
			return v.bindings.WaitHTTP(raw, remaining, every, wake)
		}); err != nil {
			panic(v.rt.NewGoError(err))
		}
		return v.rt.ToValue(true)
	})
	_ = obj.Set("signal", func(call sobek.FunctionCall) sobek.Value {
		if v.bindings.Signal == nil {
			panic(v.rt.NewGoError(errors.New("signal is not available")))
		}
		var val any
		if len(call.Arguments) > 0 && !missing(call.Argument(0)) {
			val = value.Normalize(call.Argument(0).Export())
		}
		if err := v.bindings.Signal(val); err != nil {
			panic(v.rt.NewGoError(err))
		}
		return sobek.Undefined()
	})
	_ = obj.Set("listen", func(call sobek.FunctionCall) sobek.Value {
		if v.bindings.Listen == nil || len(call.Arguments) == 0 || missing(call.Argument(0)) {
			panic(v.rt.NewGoError(errors.New("listen requires a timeout in milliseconds")))
		}
		wait := time.Duration(call.Argument(0).ToFloat()) * time.Millisecond
		if wait < 0 {
			if _, err := v.bindings.Listen(wait, nil); err != nil {
				panic(v.rt.NewGoError(err))
			}
		}
		var got any
		deadline := time.Now().Add(wait)
		err := v.Park(func(wake <-chan struct{}) (bool, error) {
			var listenErr error
			remaining := max(time.Until(deadline), 0)
			got, listenErr = v.bindings.Listen(remaining, wake)
			if errors.Is(listenErr, suite.ErrYield) {
				return true, nil
			}
			return false, listenErr
		})
		if err != nil {
			panic(v.rt.NewGoError(err))
		}
		if got == nil {
			return sobek.Null()
		}
		return v.ToValue(got)
	})
	_ = obj.Set("embed", func(call sobek.FunctionCall) sobek.Value {
		if v.bindings.Embed == nil || len(call.Arguments) == 0 || missing(call.Argument(0)) {
			panic(v.rt.NewGoError(errors.New("embed requires a value")))
		}
		var mime, name string
		if len(call.Arguments) > 1 && !missing(call.Argument(1)) {
			mime = call.Argument(1).String()
		}
		if len(call.Arguments) > 2 && !missing(call.Argument(2)) {
			name = call.Argument(2).String()
		}
		val := value.Normalize(call.Argument(0).Export())
		if err := v.bindings.Embed(val, mime, name); err != nil {
			panic(v.rt.NewGoError(err))
		}
		return sobek.Undefined()
	})
	_ = obj.Set("waitForPort", func(call sobek.FunctionCall) sobek.Value {
		if v.bindings.WaitPort == nil || len(call.Arguments) < 2 {
			panic(v.rt.NewGoError(errors.New("waitForPort requires a host and a port")))
		}
		port := int(call.Argument(1).ToFloat())
		timeout := argDuration(call.Argument(2), 30*time.Second)
		every := argDuration(call.Argument(3), 250*time.Millisecond)
		host := call.Argument(0).String()
		if err := v.parkWait(timeout, func(remaining time.Duration, wake <-chan struct{}) error {
			return v.bindings.WaitPort(host, port, remaining, every, wake)
		}); err != nil {
			panic(v.rt.NewGoError(err))
		}
		return v.rt.ToValue(true)
	})
	v.accessor("config", v.bindings.Config)
	v.accessor("info", v.bindings.Info)
	v.accessor("scenario", v.bindings.Scenario)
	v.accessor("feature", v.bindings.Feature)
	for _, name := range v.custom {
		_ = obj.Delete(name)
	}
	v.custom = nil
	for name, fn := range v.bindings.Helpers {
		n, f := name, fn
		_ = obj.Set(n, func(call sobek.FunctionCall) sobek.Value {
			args := exportArgs(call.Arguments, nil)
			got, err := f(args)
			if err != nil {
				panic(v.rt.NewGoError(err))
			}
			if got == nil {
				return sobek.Undefined()
			}
			return v.ToValue(got)
		})
		v.custom = append(v.custom, n)
	}
}

func (v *VM) parkWait(timeout time.Duration, wait func(time.Duration, <-chan struct{}) error) error {
	deadline := time.Now().Add(timeout)
	return v.Park(func(wake <-chan struct{}) (bool, error) {
		remaining := max(time.Until(deadline), 0)
		err := wait(remaining, wake)
		if errors.Is(err, suite.ErrYield) {
			return true, nil
		}
		return false, err
	})
}

func (v *VM) accessor(name string, fn func() map[string]any) {
	getter := v.rt.ToValue(func(sobek.FunctionCall) sobek.Value {
		if fn == nil {
			return v.rt.NewObject()
		}
		return v.ToValue(fn())
	})
	_ = v.karate.Delete(name)
	_ = v.karate.DefineAccessorProperty(name, getter, nil, sobek.ToFlag(true), sobek.ToFlag(true))
}

func missing(v sobek.Value) bool {
	return v == nil || sobek.IsUndefined(v) || sobek.IsNull(v)
}

func join(parts []string) string {
	return strings.Join(parts, " ")
}

func fieldDuration(v sobek.Value, key string, fallback time.Duration) time.Duration {
	if missing(v) {
		return fallback
	}
	m, ok := value.AsMap(value.Normalize(v.Export()))
	if !ok {
		return fallback
	}
	return numberDuration(m[key], fallback)
}

func argDuration(v sobek.Value, fallback time.Duration) time.Duration {
	if missing(v) {
		return fallback
	}
	return numberDuration(v.Export(), fallback)
}

func numberDuration(v any, fallback time.Duration) time.Duration {
	var n float64
	switch x := value.Normalize(v).(type) {
	case int64:
		n = float64(x)
	case float64:
		n = x
	default:
		return fallback
	}
	if n <= 0 {
		return fallback
	}
	return time.Duration(n * float64(time.Millisecond))
}
