package script

import (
	_ "embed"
	"fmt"
	"net/url"
	"strings"

	"github.com/gi-runner/gi/internal/value"
	"github.com/gi-runner/gi/internal/xml"
	"github.com/grafana/sobek"
)

// Helpers use the scenario's JS values directly so callbacks and mutations retain
// JavaScript semantics instead of round-tripping through exported Go functions.
//
//go:embed helpers.js
var helpers string

var helperProgram = mustCompileHelpers()

func mustCompileHelpers() *sobek.Program {
	p, err := sobek.Compile("karate-helpers", helpers, false)
	if err != nil {
		panic(err)
	}
	return p
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
	if _, err := v.rt.RunProgram(helperProgram); err != nil {
		return fmt.Errorf("initialize built-in helpers: %w", err)
	}
	return nil
}
