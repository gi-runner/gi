package engine

import (
	"errors"
	"fmt"
	"strings"

	"github.com/gi-runner/gi/internal/match"
	"github.com/gi-runner/gi/internal/value"
	"github.com/gi-runner/gi/internal/xml"
)

type op struct {
	text string
	mode match.Mode
}

var matchOps = []op{
	{"contains only deep", match.ContainsOnlyDeep},
	{"contains any deep", match.ContainsAnyDeep},
	{"contains only", match.ContainsOnly},
	{"contains any", match.ContainsAny},
	{"contains deep", match.ContainsDeep},
	{"!contains", match.NotContains},
	{"contains", match.Contains},
	{"!within", match.NotWithin},
	{"within", match.Within},
	{"==", match.Equals},
	{"!=", match.NotEquals},
}

func (e *exec) match(rest, doc string, sr *StepResult) error {
	each := false
	body := strings.TrimSpace(rest)
	if strings.HasPrefix(body, "each ") {
		each = true
		body = strings.TrimSpace(body[len("each "):])
	}
	lhs, mode, rhs, ok := splitMatch(body)
	if !ok {
		return errors.New("match expects: match <actual> <operator> <expected>")
	}
	if strings.TrimSpace(rhs) == "" {
		rhs = doc
	}
	if strings.TrimSpace(rhs) == "" {
		return errors.New("match is missing the expected value")
	}
	actual, err := e.evalMatchSide(lhs)
	if err != nil {
		return err
	}
	expected, err := e.eval(rhs)
	if err != nil {
		return err
	}
	if a, ok := actual.(*xml.Document); ok {
		if b, ok := expected.(*xml.Document); ok {
			actual, expected = a.Comparable(), b.Comparable()
		}
	}
	ctx := match.Context{
		Root:   actual,
		Truthy: e.vm.Truthy,
		Eval:   e.vm.Eval,
	}
	var res match.Result
	if each {
		if arr, ok := value.AsSlice(actual); ok && len(arr) == 0 && e.sess.matchEachEmpty {
			return nil
		}
		res = match.MatchEach(actual, expected, mode, ctx)
	} else {
		res = match.Match(actual, expected, mode, ctx)
	}
	if res.Pass {
		return nil
	}
	sr.Diff = res.Message
	if res.Err != nil {
		return fmt.Errorf("match validator: %w", res.Err)
	}
	if e.opt.Mock {
		return &missError{reason: firstDiffLine(res.Message)}
	}
	msg := "match failed"
	if sr.Label != "" {
		msg = sr.Label
	}
	return errors.New(msg)
}

func (e *exec) evalMatchSide(lhs string) (any, error) {
	if name, ok := strings.CutPrefix(lhs, "header "); ok {
		name = strings.TrimSpace(name)
		h, ok := e.vm.Get("responseHeaders")
		if !ok {
			return nil, errors.New("no response yet")
		}
		m, ok := value.AsMap(h)
		if !ok {
			return value.Absent{}, nil
		}
		if v, ok := m[name]; ok {
			return firstHeader(v), nil
		}
		for k, v := range m {
			if strings.EqualFold(k, name) {
				return firstHeader(v), nil
			}
		}
		return value.Absent{}, nil
	}
	if name, path, ok := splitXMLPath(lhs); ok {
		return e.queryXML(name, path)
	}
	if strings.HasPrefix(lhs, "/") {
		return e.queryXML("response", lhs)
	}
	return e.eval(lhs)
}

func splitMatch(s string) (lhs string, mode match.Mode, rhs string, ok bool) {
	for i := range topLevel(s) {
		for _, candidate := range matchOps {
			if !strings.HasPrefix(s[i:], candidate.text) || !opBounded(s, i, i+len(candidate.text), candidate.text) {
				continue
			}
			return strings.TrimSpace(s[:i]), candidate.mode, strings.TrimSpace(s[i+len(candidate.text):]), true
		}
	}
	return "", 0, "", false
}

func opBounded(s string, start, end int, text string) bool {
	if text[0] == '!' || text[0] == '=' {
		return true
	}
	if start > 0 && isWord(s[start-1]) {
		return false
	}
	if end < len(s) && isWord(s[end]) {
		return false
	}
	return true
}

func isWord(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

func firstDiffLine(s string) string {
	if before, _, ok := strings.Cut(s, "\n"); ok {
		return before
	}
	return s
}

func firstHeader(v any) any {
	if values, ok := value.AsSlice(v); ok {
		if len(values) == 0 {
			return value.Absent{}
		}
		return values[0]
	}
	return v
}
