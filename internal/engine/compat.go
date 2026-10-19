package engine

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/gi-runner/gi/internal/value"
)

func responseCookies(cookies []*http.Cookie) map[string]any {
	out := make(map[string]any, len(cookies))
	for _, c := range cookies {
		out[c.Name] = map[string]any{"name": c.Name, "value": c.Value, "domain": c.Domain, "path": c.Path, "secure": c.Secure, "httpOnly": c.HttpOnly}
	}
	return out
}

func setValues(dst url.Values, name string, v any) {
	dst.Del(name)
	if v == nil {
		return
	}
	if values, ok := value.AsSlice(v); ok {
		for _, x := range values {
			if x != nil {
				dst.Add(name, value.Sprint(x))
			}
		}
		return
	}
	dst.Set(name, value.Sprint(v))
}

func headerValues(h map[string][]string, name string, v any) map[string][]string {
	if h == nil {
		h = make(map[string][]string)
	}
	for k := range h {
		if strings.EqualFold(k, name) {
			delete(h, k)
		}
	}
	values := url.Values{}
	setValues(values, name, v)
	if list, ok := values[name]; ok {
		h[name] = list
	}
	return h
}

func (e *exec) convertStep(kind, rest, doc string) error {
	name, rhs, ok := splitAssign(rest)
	if !ok {
		return fmt.Errorf("%s expects name = value", kind)
	}
	v, err := e.eval(orDoc(rhs, doc))
	if err != nil {
		return err
	}
	text, err := replaceText(v)
	if err != nil {
		return err
	}
	if kind == "bytes" {
		if b, ok := v.([]byte); ok {
			return e.assign(name, b)
		}
		return e.assign(name, []byte(text))
	}
	if b, ok := v.([]byte); ok {
		text = string(b)
	}
	return e.assign(name, text)
}

func (e *exec) setTable(base string, rows [][]string) error {
	if name, path, ok := splitXMLPath(strings.TrimSpace(base)); ok {
		return e.setXMLTable(name, path, rows)
	}
	if len(rows) < 2 || len(rows[0]) < 2 || rows[0][0] != "path" {
		return errors.New("set table requires path and value columns")
	}
	for _, row := range rows[1:] {
		if len(row) == 0 {
			continue
		}
		for i := 1; i < len(row); i++ {
			if strings.TrimSpace(row[i]) == "" {
				continue
			}
			if i >= len(rows[0]) {
				return errors.New("set table row has too many cells")
			}
			path := strings.TrimSpace(base)
			if rows[0][i] != "value" {
				path += "[" + rows[0][i] + "]"
			}
			if row[0] != "" {
				if !strings.HasPrefix(row[0], "[") {
					path += "."
				}
				path += row[0]
			}
			v, err := e.eval(row[i])
			if err != nil {
				return err
			}
			if err := e.vm.SetPath(path, v); err != nil {
				return err
			}
		}
	}
	return nil
}
