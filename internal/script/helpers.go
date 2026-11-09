package script

import (
	_ "embed"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"

	"github.com/gi-runner/gi/internal/match"
	"github.com/gi-runner/gi/internal/value"
	"github.com/gi-runner/gi/internal/xml"
	"github.com/grafana/sobek"
)

// Helpers use the scenario's JS values directly so callbacks and mutations retain
// JavaScript semantics instead of round-tripping through exported Go functions.
//go:embed helpers.js
var helpers string

var helperProgram = mustCompileHelpers()

var builtins = sync.OnceValue(func() map[string]bool {
	v := New()
	out := map[string]bool{}
	if v.Bind(Bindings{}) != nil || v.initializeAPI() != nil {
		return out
	}
	for _, k := range v.karate.Keys() {
		out[k] = true
	}
	for _, k := range []string{"config", "info", "scenario", "feature", "env"} {
		out[k] = true
	}
	return out
})

// Builtin reports whether name is part of the karate object.
func Builtin(name string) bool { return builtins()[name] }

var modes = map[string]match.Mode{
	"==": match.Equals, "contains": match.Contains, "contains deep": match.ContainsDeep,
	"contains only": match.ContainsOnly, "contains any": match.ContainsAny,
}

func mustCompileHelpers() *sobek.Program {
	p, err := sobek.Compile("karate-helpers", helpers, false)
	if err != nil {
		panic(err)
	}
	return p
}

// Properties is the process environment, plus karate.env when --env is set.
func Properties(env string) map[string]any {
	out := map[string]any{}
	for _, kv := range os.Environ() {
		k, v, _ := strings.Cut(kv, "=")
		out[k] = v
	}
	if env != "" {
		out["karate.env"] = env
	}
	return out
}

func lookupProp(name, env string) (string, bool) {
	if name == "karate.env" && env != "" {
		return env, true
	}
	return os.LookupEnv(name)
}

func (v *VM) bindHelpers(obj *sobek.Object) error {
	v.bindPathHelpers(obj)
	_ = obj.Set("uuid", v.rt.Get("uuid"))
	_ = obj.Set("urlEncode", url.QueryEscape)
	_ = obj.Set("urlDecode", url.QueryUnescape)
	_ = obj.Set("get", func(call sobek.FunctionCall) sobek.Value {
		got, err := v.Eval(call.Argument(0).String())
		if err != nil || value.IsAbsent(got) {
			return call.Argument(1)
		}
		return v.rt.ToValue(got)
	})
	_ = obj.Set("remove", func(call sobek.FunctionCall) sobek.Value {
		path := call.Argument(0).String()
		if len(call.Arguments) > 1 {
			path += "." + strings.TrimPrefix(call.Argument(1).String(), "$.")
		}
		if err := v.RemovePath(path); err != nil {
			panic(v.rt.NewGoError(err))
		}
		return sobek.Undefined()
	})
	_ = obj.Set("set", func(call sobek.FunctionCall) sobek.Value {
		name := call.Argument(0).String()
		if len(call.Arguments) == 3 {
			path := strings.TrimPrefix(call.Argument(1).String(), "$")
			if !strings.HasPrefix(path, ".") && !strings.HasPrefix(path, "[") {
				path = "." + path
			}
			if err := v.SetPath(name+path, call.Argument(2).Export()); err != nil {
				panic(v.rt.NewGoError(err))
			}
		} else {
			v.Set(name, call.Argument(1).Export())
		}
		return sobek.Undefined()
	})
	for _, name := range []string{"fromJson", "fromString", "toJson"} {
		_ = obj.Set(name, func(call sobek.FunctionCall) sobek.Value {
			x := call.Argument(0)
			raw := value.Normalize(x.Export())
			if name == "toJson" {
				if model, ok := raw.(interface{ DataValue() any }); ok {
					return v.ToValue(model.DataValue())
				}
			}
			text, isString := raw.(string)
			if !isString {
				return x
			}
			trimmed := strings.TrimSpace(text)
			if name == "fromString" {
				if strings.HasPrefix(trimmed, "<") {
					doc, err := xml.ParseDocument(text)
					if err != nil {
						panic(v.rt.NewGoError(err))
					}
					return v.ToValue(doc)
				}
				if !strings.HasPrefix(trimmed, "{") && !strings.HasPrefix(trimmed, "[") {
					return x
				}
			}
			got, err := value.Parse([]byte(text))
			if err != nil {
				panic(v.rt.NewGoError(err))
			}
			return v.ToValue(got)
		})
	}
	_ = obj.Set("readAsString", func(ref string) (string, error) { b, err := v.Bytes(ref); return string(b), err })
	_ = obj.Set("readAsBytes", v.Bytes)
	_ = obj.Set("toBytes", func(x sobek.Value) []byte { return []byte(x.String()) })
	_ = obj.Set("fromCsv", value.ParseCSV)
	_ = obj.Set("sysprop", func(call sobek.FunctionCall) sobek.Value {
		if missing(call.Argument(0)) {
			panic(v.rt.NewGoError(errors.New("sysprop needs a name")))
		}
		got, ok := lookupProp(call.Argument(0).String(), v.bindings.Env)
		if ok {
			return v.rt.ToValue(got)
		}
		if len(call.Arguments) > 1 {
			return call.Argument(1)
		}
		return sobek.Null()
	})
	initializer, err := v.rt.RunProgram(helperProgram)
	if err != nil {
		return fmt.Errorf("initialize built-in helpers: %w", err)
	}
	fn, ok := sobek.AssertFunction(initializer)
	if !ok {
		return errors.New("built-in helper initializer is not a function")
	}
	native := v.object(map[string]any{
		"match": func(actual, expected sobek.Value, op string) (map[string]any, error) {
			mode, ok := modes[op]
			if !ok {
				return nil, fmt.Errorf("unknown match operator %q", op)
			}
			res := v.compare(actual, expected, mode)
			return matchResult(res.Pass, res.Message), res.Err
		},
		"text": func(x sobek.Value) any {
			switch raw := x.Export().(type) {
			case string:
				return raw
			case []byte:
				return string(raw)
			default:
				return nil
			}
		},
	})
	if _, err := fn(sobek.Undefined(), obj, native); err != nil {
		return fmt.Errorf("initialize built-in helpers: %w", err)
	}
	return nil
}
