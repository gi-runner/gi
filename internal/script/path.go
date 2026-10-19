package script

import (
	"fmt"
	"strings"

	"github.com/gi-runner/gi/internal/value"
	"github.com/gi-runner/gi/internal/xml"
	"github.com/grafana/sobek"
	"github.com/ohler55/ojg/jp"
)

func JSONPath(data any, path string) (any, error) {
	expr, err := jp.ParseString(path)
	if err != nil {
		return nil, fmt.Errorf("JSONPath %q: %w", path, err)
	}
	results := expr.Get(value.Normalize(data))
	if expr.Normal() {
		if len(results) == 0 {
			return value.Absent{}, nil
		}
		return results[0], nil
	}
	if results == nil {
		results = []any{}
	}
	return results, nil
}

func (v *VM) advancedPath(expr string) (any, bool, error) {
	i := strings.IndexAny(expr, ".[")
	if i < 1 {
		return nil, false, nil
	}
	root, path := expr[:i], expr[i:]
	if open, filter := strings.IndexByte(path, '('), strings.Index(path, "[?"); open >= 0 && (filter < 0 || open < filter) {
		return nil, false, nil
	}
	if !strings.Contains(path, "..") && !strings.ContainsAny(path, ":,") && !strings.Contains(path, "[?") && !strings.Contains(path, "[-") {
		return nil, false, nil
	}
	if _, ok := parsePath(root); !ok {
		return nil, false, nil
	}
	data, ok := v.Get(root)
	if !ok {
		return nil, true, fmt.Errorf("undefined variable %s", root)
	}
	result, err := JSONPath(data, "$"+path)
	return result, true, err
}

func (v *VM) bindPathHelpers(obj *sobek.Object) {
	_ = obj.Set("jsonPath", func(call sobek.FunctionCall) sobek.Value {
		got, err := JSONPath(call.Argument(0).Export(), call.Argument(1).String())
		if err != nil {
			panic(v.rt.NewGoError(err))
		}
		return v.ToValue(got)
	})
	_ = obj.Set("xmlPath", func(call sobek.FunctionCall) sobek.Value {
		raw := value.Normalize(call.Argument(0).Export())
		doc, ok := raw.(*xml.Document)
		if !ok {
			var err error
			doc, err = xml.ParseDocument(value.Sprint(raw))
			if err != nil {
				panic(v.rt.NewGoError(err))
			}
		}
		got, err := doc.Query(call.Argument(1).String())
		if err != nil {
			panic(v.rt.NewGoError(err))
		}
		return v.ToValue(got)
	})
}
