package engine

import (
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"sync"

	"github.com/gi-runner/gi/internal/script"
	"github.com/gi-runner/gi/internal/suite"
	"github.com/gi-runner/gi/internal/value"
)

// MockSpec describes a mock a scenario starts with karate.start.
type MockSpec struct {
	Path       string
	Port       int
	SSL        bool
	Cert       string
	Key        string
	Arg        map[string]any
	PathPrefix string
}

var props = map[string]func(*exec) any{
	"tags":            func(e *exec) any { return e.tags() },
	"tagValues":       func(e *exec) any { return e.tagValues() },
	"properties":      func(e *exec) any { return script.Properties(e.opt.Env) },
	"scenarioOutline": (*exec).outlineInfo,
	"prevRequest": func(e *exec) any {
		if e.sess.prev == nil {
			return nil
		}
		return e.sess.prev
	},
	"request":  (*exec).mockRequest,
	"response": (*exec).response,
}

func (e *exec) props() map[string]func() any {
	out := make(map[string]func() any, len(props))
	for name, fn := range props {
		out[name] = func() any { return fn(e) }
	}
	return out
}

func (e *exec) tags() []any {
	if e.scenario == nil {
		return []any{}
	}
	out := []any{}
	seen := map[string]bool{}
	for _, t := range e.scenario.Tags {
		t = strings.TrimPrefix(t, "@")
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

// tagValues lets the first scenario tag of a name replace the feature's values.
func (e *exec) tagValues() map[string]any {
	out := map[string]any{}
	if e.scenario == nil {
		return out
	}
	own := len(e.scenario.Tags) - e.scenario.OwnTags
	replaced := map[string]bool{}
	for i, t := range e.scenario.Tags {
		name, raw, _ := strings.Cut(strings.TrimPrefix(t, "@"), "=")
		vals := []any{}
		if raw != "" {
			for v := range strings.SplitSeq(raw, ",") {
				vals = append(vals, v)
			}
		}
		prior, _ := out[name].([]any)
		if i >= own && !replaced[name] {
			replaced[name] = true
			prior = nil
		}
		out[name] = append(prior, vals...)
	}
	return out
}

func (e *exec) outlineInfo() any {
	if e.scenario == nil || e.scenario.Examples == nil {
		return nil
	}
	ex := e.scenario.Examples
	tables := make([]any, len(ex.Tables))
	for i, t := range ex.Tables {
		tables[i] = t
	}
	return map[string]any{
		"name": ex.Name, "description": ex.Description, "line": ex.Line,
		"sectionIndex": e.scenario.Section, "exampleTableCount": len(ex.Tables),
		"exampleTables": tables, "numScenariosToExecute": ex.Scenarios,
	}
}

func (e *exec) mockRequest() any {
	if !e.opt.Mock {
		return nil
	}
	got, _ := e.vm.Get("request")
	return got
}

func (e *exec) response() any {
	body, ok := e.vm.Get("response")
	if e.opt.Mock || !ok {
		return body
	}
	status, _ := e.vm.Get("responseStatus")
	took, _ := e.vm.Get("responseTime")
	headers, _ := e.vm.Get("responseHeaders")
	hm, _ := value.AsMap(headers)
	return e.vm.Object(
		map[string]any{"status": status, "body": body, "headers": headers, "responseTime": took},
		map[string]func([]any) (any, error){"header": func(args []any) (any, error) {
			if len(args) == 0 {
				return nil, errors.New("header needs a name")
			}
			name := http.CanonicalHeaderKey(value.Sprint(args[0]))
			for k, v := range hm {
				if http.CanonicalHeaderKey(k) == name {
					if list, ok := value.AsSlice(v); ok && len(list) > 0 {
						return list[0], nil
					}
					return v, nil
				}
			}
			return nil, nil
		}},
	)
}

func (e *exec) startMock(arg any) (script.Server, error) {
	if e.opt.StartMock == nil {
		return script.Server{}, errors.New("start is not available")
	}
	spec := MockSpec{}
	switch x := arg.(type) {
	case string:
		spec.Path = x
	case map[string]any:
		spec.Path, _ = x["mock"].(string)
		if spec.Path == "" {
			return script.Server{}, errors.New("start options need a mock feature path")
		}
		spec.Port, _ = value.AsInt(x["port"])
		spec.SSL, _ = x["ssl"].(bool)
		spec.Cert, _ = x["cert"].(string)
		spec.Key, _ = x["key"].(string)
		spec.Arg, _ = value.AsMap(x["arg"])
		spec.PathPrefix, _ = x["pathPrefix"].(string)
	default:
		return script.Server{}, fmt.Errorf("start expects a path or options, got %T", arg)
	}
	dir := filepath.Dir(e.file.Path)
	spec.Path = suite.Resolve(e.root(), dir, spec.Path)
	for _, p := range []*string{&spec.Cert, &spec.Key} {
		if *p != "" {
			*p = suite.Resolve(e.root(), dir, *p)
		}
	}
	srv, err := e.opt.StartMock(e.ctx, spec)
	if err != nil {
		return script.Server{}, err
	}
	srv.Stop = sync.OnceValue(srv.Stop)
	e.track(srv.Stop)
	return srv, nil
}

func (e *exec) track(closeFn func() error) {
	e.links = append(e.links, Link{Close: closeFn})
}
