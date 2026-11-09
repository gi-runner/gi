package script

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/gi-runner/gi/internal/match"
	"github.com/gi-runner/gi/internal/suite"
	"github.com/gi-runner/gi/internal/value"
	"github.com/gi-runner/gi/internal/xml"
	"github.com/grafana/sobek"
)

func (v *VM) bindAPI(obj *sobek.Object) {
	_ = obj.Set("extract", func(text, pattern string, group int) (sobek.Value, error) {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, err
		}
		m := re.FindStringSubmatch(text)
		if m == nil {
			return sobek.Null(), nil
		}
		g, err := regexGroup(m, group)
		return v.rt.ToValue(g), err
	})
	_ = obj.Set("extractAll", func(text, pattern string, group int) ([]any, error) {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, err
		}
		out := []any{}
		for _, m := range re.FindAllStringSubmatch(text, -1) {
			g, err := regexGroup(m, group)
			if err != nil {
				return nil, err
			}
			out = append(out, g)
		}
		return out, nil
	})
	_ = obj.Set("prettyXml", func(call sobek.FunctionCall) sobek.Value {
		doc, err := asDocument(call.Argument(0))
		if err != nil {
			panic(v.rt.NewGoError(fmt.Errorf("prettyXml: %w", err)))
		}
		return v.rt.ToValue(doc.Pretty())
	})
	_ = obj.Set("toAbsolutePath", func(path string) (string, error) {
		return filepath.Abs(suite.Resolve(v.root, v.featureDir, path))
	})
	_ = obj.Set("sysenv", func(call sobek.FunctionCall) sobek.Value {
		fallback := call.Argument(1)
		if missing(call.Argument(0)) {
			return fallback
		}
		if got := os.Getenv(call.Argument(0).String()); got != "" {
			return v.rt.ToValue(got)
		}
		if len(call.Arguments) < 2 {
			return sobek.Null()
		}
		return fallback
	})
	_ = obj.Set("eval", func(src string) (sobek.Value, error) { return v.run(src) })
	_ = obj.Set("setXml", func(call sobek.FunctionCall) sobek.Value {
		if err := v.setXML(call.Arguments); err != nil {
			panic(v.rt.NewGoError(fmt.Errorf("setXml: %w", err)))
		}
		return sobek.Undefined()
	})
	_ = obj.Set("match", func(call sobek.FunctionCall) sobek.Value {
		got, err := v.matchCall(call.Arguments)
		if err != nil {
			panic(v.rt.NewGoError(err))
		}
		return v.ToValue(got)
	})
	v.lazy(obj, "logger", func() map[string]any { return v.logger() })
	v.lazy(obj, "faker", func() map[string]any { return v.faker() })
	v.lazy(obj, "os", func() map[string]any { return map[string]any{"name": osName(), "type": osType()} })
}

// lazy builds a namespace object on first read so unused helpers cost nothing per iteration.
func (v *VM) lazy(obj *sobek.Object, name string, build func() map[string]any) {
	var built *sobek.Object
	getter := v.rt.ToValue(func(sobek.FunctionCall) sobek.Value {
		if built == nil {
			built = v.object(build())
		}
		return built
	})
	_ = obj.DefineAccessorProperty(name, getter, nil, sobek.FLAG_TRUE, sobek.FLAG_TRUE)
}

func (v *VM) object(props map[string]any) *sobek.Object {
	obj := v.rt.NewObject()
	for k, x := range props {
		_ = obj.Set(k, x)
	}
	return obj
}

func regexGroup(m []string, group int) (string, error) {
	if group < 0 || group >= len(m) {
		return "", fmt.Errorf("regex has no group %d", group)
	}
	return m[group], nil
}

func asDocument(arg sobek.Value) (*xml.Document, error) {
	switch x := value.Normalize(arg.Export()).(type) {
	case *xml.Document:
		return x, nil
	case string:
		return xml.ParseDocument(x)
	default:
		return nil, errors.New("argument must be XML or a string")
	}
}

func (v *VM) setXML(args []sobek.Value) error {
	if len(args) < 2 {
		return errors.New("needs a name and XML")
	}
	name := args[0].String()
	doc, err := xml.ParseDocument(args[len(args)-1].String())
	if err != nil {
		return err
	}
	if len(args) == 2 {
		v.Set(name, doc)
		return nil
	}
	target, _ := v.Get(name)
	root, ok := target.(*xml.Document)
	if !ok {
		return fmt.Errorf("%s is not XML", name)
	}
	return root.Set(args[1].String(), doc)
}

func (v *VM) matchCall(args []sobek.Value) (map[string]any, error) {
	switch {
	case len(args) == 0:
		return nil, errors.New("match needs an expression or two values")
	case len(args) == 1:
		if v.bindings.MatchExpr == nil {
			return nil, errors.New("match is not available here")
		}
		pass, msg, err := v.bindings.MatchExpr(args[0].String())
		if err != nil {
			return nil, err
		}
		return matchResult(pass, msg), nil
	default:
		res := v.compare(args[0], args[1], match.Equals)
		if res.Err != nil {
			return nil, res.Err
		}
		return matchResult(res.Pass, res.Message), nil
	}
}

func (v *VM) compare(actual, expected sobek.Value, mode match.Mode) match.Result {
	return match.Match(v.export(actual), v.export(expected), mode, match.Context{Truthy: v.Truthy, Eval: v.Eval})
}

func matchResult(pass bool, msg string) map[string]any {
	out := map[string]any{"pass": pass, "message": nil}
	if !pass {
		out["message"] = msg
	}
	return out
}

func (v *VM) logger() map[string]any {
	out := make(map[string]any, 5)
	for _, level := range []string{"trace", "debug", "info", "warn", "error"} {
		prefix := strings.ToUpper(level) + " "
		if level == "info" {
			prefix = ""
		}
		out[level] = func(call sobek.FunctionCall) sobek.Value {
			if v.bindings.Log == nil {
				return sobek.Undefined()
			}
			parts := make([]string, len(call.Arguments))
			for i, arg := range call.Arguments {
				parts[i] = "null"
				if !missing(arg) {
					parts[i] = value.Format(value.Normalize(arg.Export()))
				}
			}
			if err := v.bindings.Log(prefix + join(parts)); err != nil {
				panic(v.rt.NewGoError(err))
			}
			return sobek.Undefined()
		}
	}
	return out
}

func osType() string {
	switch runtime.GOOS {
	case "windows":
		return "windows"
	case "darwin":
		return "macosx"
	case "linux":
		return "linux"
	default:
		return "unknown"
	}
}

func osName() string {
	switch runtime.GOOS {
	case "windows":
		return "Windows"
	case "darwin":
		return "Mac OS X"
	case "linux":
		return "Linux"
	default:
		return runtime.GOOS
	}
}
