package engine

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/gi-runner/gi/internal/feature"
	"github.com/gi-runner/gi/internal/value"
)

// stepCall is one step split into its keyword and the text after it.
type stepCall struct {
	ctx  context.Context //nolint:containedctx // lives for one step
	cmd  string
	rest string
	doc  string
	st   feature.Step
	sr   *StepResult
}

// body is the step argument, or the doc string when the line has none.
func (c stepCall) body() string { return orDoc(c.rest, c.doc) }

type keyword func(e *exec, c stepCall) error

// keywords is filled in init because handlers reach back into step through call.
var keywords map[string]keyword

func init() {
	keywords = map[string]keyword{
		"url":         (*exec).urlStep,
		"path":        (*exec).pathStep,
		"param":       (*exec).paramStep,
		"params":      (*exec).paramsStep,
		"header":      (*exec).headerStep,
		"headers":     (*exec).headersStep,
		"cookie":      (*exec).cookieStep,
		"cookies":     func(e *exec, c stepCall) error { return e.cookies(c.body()) },
		"form":        func(e *exec, c stepCall) error { return e.formStep(c.rest, c.doc) },
		"multipart":   func(e *exec, c stepCall) error { return e.multipart(c.rest, c.doc) },
		"request":     (*exec).requestStep,
		"soap":        (*exec).soapStep,
		"method":      func(e *exec, c stepCall) error { return e.method(c.ctx, c.rest, c.sr) },
		"status":      (*exec).statusStep,
		"match":       func(e *exec, c stepCall) error { return e.match(c.rest, c.doc, c.sr) },
		"def":         func(e *exec, c stepCall) error { return e.def(c.ctx, c.rest, c.doc) },
		"eval":        func(e *exec, c stepCall) error { _, err := e.eval(c.body()); return err },
		"string":      func(e *exec, c stepCall) error { return e.convertStep(c.cmd, c.rest, c.doc) },
		"bytes":       func(e *exec, c stepCall) error { return e.convertStep(c.cmd, c.rest, c.doc) },
		"text":        func(e *exec, c stepCall) error { return e.textStep(c.rest, c.doc) },
		"json":        func(e *exec, c stepCall) error { return e.jsonStep(c.rest, c.doc) },
		"yaml":        func(e *exec, c stepCall) error { return e.yamlStep(c.rest, c.doc) },
		"csv":         func(e *exec, c stepCall) error { return e.csvStep(c.rest, c.doc) },
		"xml":         func(e *exec, c stepCall) error { return e.xmlStep(c.rest, c.doc) },
		"xmlstring":   func(e *exec, c stepCall) error { return e.xmlStringStep(c.rest, c.doc) },
		"driver":      func(e *exec, c stepCall) error { return e.driverStep(c.ctx, c.body()) },
		"click":       func(e *exec, c stepCall) error { return e.clickStep(c.ctx, c.body()) },
		"input":       func(e *exec, c stepCall) error { return e.inputStep(c.ctx, c.rest) },
		"copy":        func(e *exec, c stepCall) error { return e.copyStep(c.rest) },
		"table":       func(e *exec, c stepCall) error { return e.tableStep(c.rest, c.st.Rows) },
		"replace":     func(e *exec, c stepCall) error { return e.replaceStep(c.rest, c.st.Rows) },
		"assert":      func(e *exec, c stepCall) error { return e.assertStep(c.body()) },
		"print":       func(e *exec, c stepCall) error { return e.print(c.body(), c.sr) },
		"configure":   func(e *exec, c stepCall) error { return e.configure(c.rest) },
		"set":         (*exec).setStep,
		"remove":      (*exec).removeStep,
		"call":        (*exec).callStep,
		"callonce":    (*exec).callStep,
		"retry":       (*exec).retryStep,
		"methodis":    func(e *exec, c stepCall) error { return e.methodIs(c.rest) },
		"pathmatches": func(e *exec, c stepCall) error { return e.pathMatches(c.rest) },
	}
}

// Keyword reports whether word starts a built-in step.
func Keyword(word string) bool { return keywords[strings.ToLower(word)] != nil }

func (e *exec) step(ctx context.Context, st feature.Step, sr *StepResult) error {
	text := strings.TrimSpace(st.Text)
	cmd, rest, _ := strings.Cut(text, " ")
	c := stepCall{ctx: ctx, cmd: strings.ToLower(cmd), rest: strings.TrimSpace(rest), doc: st.DocString, st: st, sr: sr}
	if run := keywords[c.cmd]; run != nil {
		return run(e, c)
	}
	if handled, err := e.customStep(ctx, text, st); handled {
		return err
	}
	_, err := e.eval(orDoc(text, st.DocString))
	return err
}

func (e *exec) urlStep(c stepCall) error {
	v, err := e.eval(c.body())
	if err != nil {
		return err
	}
	e.sess.baseURL = value.Sprint(v)
	return nil
}

func (e *exec) pathStep(c stepCall) error {
	args, err := e.evalArgs(c.body())
	if err != nil {
		return err
	}
	var appendPath func(any)
	appendPath = func(v any) {
		if v == nil {
			return
		}
		if list, ok := value.AsSlice(v); ok {
			for _, item := range list {
				appendPath(item)
			}
			return
		}
		e.sess.paths = append(e.sess.paths, value.Sprint(v))
	}
	for _, arg := range args {
		appendPath(arg)
	}
	return nil
}

func (e *exec) paramStep(c stepCall) error {
	name, expr, ok := splitAssign(c.rest)
	if !ok {
		return errors.New("param expects name = value")
	}
	v, err := e.eval(expr)
	if err != nil {
		return err
	}
	if e.sess.params == nil {
		e.sess.params = url.Values{}
	}
	setValues(e.sess.params, strings.TrimSpace(name), v)
	return nil
}

func (e *exec) paramsStep(c stepCall) error {
	m, err := e.evalObject(c.body(), "params")
	if err != nil {
		return err
	}
	if e.sess.params == nil {
		e.sess.params = url.Values{}
	}
	for k, child := range m {
		setValues(e.sess.params, k, child)
	}
	return nil
}

func (e *exec) headerStep(c stepCall) error {
	name, expr, ok := splitAssign(c.rest)
	if !ok {
		return errors.New("header expects Name = value")
	}
	v, err := e.eval(expr)
	if err != nil {
		return err
	}
	e.sess.headers = headerValues(e.sess.headers, strings.TrimSpace(name), v)
	return nil
}

func (e *exec) headersStep(c stepCall) error {
	m, err := e.evalObject(c.body(), "headers")
	if err != nil {
		return err
	}
	for k, child := range m {
		e.sess.headers = headerValues(e.sess.headers, k, child)
	}
	return nil
}

func (e *exec) evalObject(expr, what string) (map[string]any, error) {
	v, err := e.eval(expr)
	if err != nil {
		return nil, err
	}
	m, ok := value.AsMap(v)
	if !ok {
		return nil, fmt.Errorf("%s expects an object", what)
	}
	return m, nil
}

func (e *exec) cookieStep(c stepCall) error {
	name, expr, ok := splitAssign(c.rest)
	if !ok {
		return errors.New("cookie expects name = value")
	}
	v, err := e.eval(expr)
	if err != nil {
		return err
	}
	e.sess.cookies = append(e.sess.cookies, [2]string{strings.TrimSpace(name), cookieText(v)})
	return nil
}

func (e *exec) requestStep(c stepCall) error {
	v, err := e.eval(c.body())
	if err != nil {
		return err
	}
	e.sess.body = v
	e.sess.hasBody = true
	return nil
}

func (e *exec) soapStep(c stepCall) error {
	action, ok := strings.CutPrefix(c.rest, "action")
	if !ok {
		return errors.New("soap expects action <expression>")
	}
	var v any
	if strings.TrimSpace(action) != "" {
		var err error
		if v, err = e.eval(action); err != nil {
			return err
		}
	}
	e.sess.headers = headerSet(e.sess.headers, "SOAPAction", value.Sprint(v))
	e.sess.headers = headerSet(e.sess.headers, "Content-Type", "text/xml")
	return e.method(c.ctx, "POST", c.sr)
}

func (e *exec) statusStep(c stepCall) error {
	v, err := e.eval(c.rest)
	if err != nil {
		return err
	}
	want, ok := value.AsInt(v)
	if !ok {
		return fmt.Errorf("status expects a number, got %s", value.Kind(v))
	}
	got, _ := e.vm.Get("responseStatus")
	have, ok := value.AsInt(got)
	if !ok {
		return errors.New("no response yet")
	}
	if have != want {
		return fmt.Errorf("status %d != %d", have, want)
	}
	return nil
}

func (e *exec) setStep(c stepCall) error {
	if len(c.st.Rows) > 0 {
		return e.setTable(c.rest, c.st.Rows)
	}
	lhs, rhs, ok := splitAssign(c.rest)
	if !ok {
		return errors.New("set expects path = value")
	}
	if strings.TrimSpace(rhs) == "" {
		rhs = c.doc
	}
	v, err := e.eval(rhs)
	if err != nil {
		return err
	}
	if name, path, ok := splitXMLPath(strings.TrimSpace(lhs)); ok {
		return e.mutateXML(name, path, v, false)
	}
	return e.vm.SetPath(strings.TrimSpace(lhs), v)
}

func (e *exec) removeStep(c stepCall) error {
	path := strings.TrimSpace(c.body())
	if name, xpath, ok := splitXMLPath(path); ok {
		return e.mutateXML(name, xpath, nil, true)
	}
	return e.vm.RemovePath(path)
}

func (e *exec) callStep(c stepCall) error {
	_, err := e.call(c.ctx, c.body(), c.cmd == "callonce", true)
	return err
}

func (e *exec) retryStep(c stepCall) error {
	expr, ok := strings.CutPrefix(c.rest, "until ")
	if !ok {
		return errors.New("retry expects until <expression>")
	}
	e.vm.SetRetryUntil(strings.TrimSpace(expr))
	return nil
}
