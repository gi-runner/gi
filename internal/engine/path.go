package engine

import (
	"fmt"
	"github.com/gi-runner/gi/internal/feature"
	"strconv"
	"strings"

	"github.com/gi-runner/gi/internal/value"
	"github.com/gi-runner/gi/internal/xml"
	"github.com/ohler55/ojg/jp"
)

func splitXMLPath(expr string) (string, string, bool) {
	name, path, ok := strings.Cut(strings.TrimSpace(expr), " ")
	path = strings.TrimSpace(path)
	if !ok || !ident(name) {
		return "", "", false
	}
	xpathFunction := false
	for _, prefix := range []string{"count(", "string(", "number(", "boolean(", "sum(", "normalize-space(", "local-name(", "namespace-uri(", "name("} {
		if strings.HasPrefix(path, prefix) {
			xpathFunction = true
			break
		}
	}
	return name, path, strings.HasPrefix(path, "/") || xpathFunction
}
func (e *exec) queryXML(name, path string) (any, error) {
	val, ok := e.vm.Get(name)
	if !ok {
		return nil, fmt.Errorf("undefined variable %s", name)
	}
	doc, ok := val.(*xml.Document)
	if !ok {
		return nil, fmt.Errorf("%s is not XML", name)
	}
	return doc.Query(path)
}
func (e *exec) mutateXML(name, path string, val any, remove bool) error {
	raw, ok := e.vm.Get(name)
	if !ok {
		return fmt.Errorf("undefined variable %s", name)
	}
	doc, ok := raw.(*xml.Document)
	if !ok {
		return fmt.Errorf("%s is not XML", name)
	}
	if remove {
		return doc.Remove(path)
	}
	return doc.Set(path, val)
}
func (e *exec) getPath(expr string) (any, error) {
	if name, path, ok := splitXMLPath(expr); ok {
		return e.queryXML(name, path)
	}
	name, path, ok := strings.Cut(strings.TrimSpace(expr), " ")
	if !ok {
		return e.eval(expr)
	}
	raw, exists := e.vm.Get(name)
	if !exists {
		return nil, fmt.Errorf("undefined variable %s", name)
	}
	parsed, err := jp.ParseString(strings.TrimSpace(path))
	if err != nil {
		return nil, err
	}
	result := parsed.Get(raw)
	if len(result) == 0 {
		return value.Absent{}, nil
	}
	if parsed.Normal() {
		return result[0], nil
	}
	return result, nil
}

func callLines(path string) (string, map[int]bool, error) {
	start := strings.Index(path, ".feature:")
	if start < 0 {
		return path, nil, nil
	}
	base, tail := path[:start+len(".feature")], path[start+len(".feature:"):]
	lines := make(map[int]bool)
	for raw := range strings.SplitSeq(tail, ":") {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			return "", nil, fmt.Errorf("invalid feature line selector %q", tail)
		}
		lines[n] = true
	}
	return base, lines, nil
}
func selectedLine(sc feature.Scenario, lines map[int]bool) bool {
	return len(lines) == 0 || lines[sc.Line] || lines[sc.OutlineID]
}
