package engine

import (
	"errors"
	"net/url"
	"strings"

	"github.com/gi-runner/gi/internal/value"
)

func (e *exec) formStep(rest, doc string) error {
	rest = strings.TrimSpace(rest)
	if expr, ok := strings.CutPrefix(rest, "fields"); ok {
		expr = strings.TrimSpace(expr)
		if expr == "" {
			expr = doc
		}
		return e.formFields(expr)
	}
	if assign, ok := strings.CutPrefix(rest, "field "); ok {
		return e.formField(assign)
	}
	return errors.New("form expects field or fields")
}

func (e *exec) formField(assign string) error {
	name, expr, ok := splitAssign(assign)
	if !ok {
		return errors.New("form field expects name = value")
	}
	v, err := e.eval(expr)
	if err != nil {
		return err
	}
	e.addForm(strings.TrimSpace(name), v)
	return nil
}

func (e *exec) formFields(expr string) error {
	v, err := e.eval(expr)
	if err != nil {
		return err
	}
	m, ok := value.AsMap(v)
	if !ok {
		return errors.New("form fields expects an object")
	}
	for k, child := range m {
		e.addForm(k, child)
	}
	return nil
}

func (e *exec) addForm(name string, v any) {
	if v == nil || name == "" {
		return
	}
	if e.sess.form == nil {
		e.sess.form = url.Values{}
	}
	if items, ok := value.AsSlice(v); ok {
		for _, item := range items {
			if item != nil {
				e.sess.form.Add(name, value.Sprint(item))
			}
		}
		return
	}
	e.sess.form.Add(name, value.Sprint(v))
}

func addQuery(raw string, form url.Values) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	q := u.Query()
	for k, vs := range form {
		for _, v := range vs {
			q.Add(k, v)
		}
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}
