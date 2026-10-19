package engine

import (
	"bytes"
	"errors"
	"fmt"
	"mime"
	"mime/multipart"
	"net/textproto"
	"path"
	"strings"

	"github.com/gi-runner/gi/internal/value"
)

type part struct {
	name     string
	filename string
	ctype    string
	body     []byte
}

func (e *exec) cookies(expr string) error {
	v, err := e.eval(expr)
	if err != nil {
		return err
	}
	m, ok := value.AsMap(v)
	if !ok {
		return errors.New("cookies expects an object")
	}
	for k, child := range m {
		e.sess.cookies = append(e.sess.cookies, [2]string{k, cookieText(child)})
	}
	return nil
}

func cookieText(v any) string {
	if m, ok := value.AsMap(v); ok {
		if inner, ok := m["value"]; ok {
			return value.Sprint(inner)
		}
	}
	return value.Sprint(v)
}

func (e *exec) multipart(rest, doc string) error {
	rest = strings.TrimSpace(rest)
	kind, expr, ok := strings.Cut(rest, " ")
	if !ok {
		kind, expr = rest, ""
	}
	expr = strings.TrimSpace(expr)
	if expr == "" {
		expr = doc
	}
	switch kind {
	case "field":
		return e.multipartField(expr)
	case "fields":
		return e.multipartFields(expr)
	case "file":
		return e.multipartFile(expr)
	case "files":
		return e.multipartFiles(expr)
	default:
		return errors.New("multipart expects field, fields, file, or files")
	}
}

func (e *exec) multipartField(assign string) error {
	name, expr, ok := splitAssign(assign)
	if !ok {
		return errors.New("multipart field expects name = value")
	}
	v, err := e.eval(expr)
	if err != nil {
		return err
	}
	return e.addValue(strings.TrimSpace(name), "", "", v)
}

func (e *exec) multipartFields(expr string) error {
	v, err := e.eval(expr)
	if err != nil {
		return err
	}
	m, ok := value.AsMap(v)
	if !ok {
		return errors.New("multipart fields expects an object")
	}
	for k, child := range m {
		if spec, ok := value.AsMap(child); ok && partSpec(spec) {
			if err := e.addSpec(k, spec); err != nil {
				return err
			}
			continue
		}
		if err := e.addValue(k, "", "", child); err != nil {
			return err
		}
	}
	return nil
}

func (e *exec) multipartFile(assign string) error {
	name, expr, ok := splitAssign(assign)
	if !ok {
		return errors.New("multipart file expects name = value")
	}
	v, err := e.eval(expr)
	if err != nil {
		return err
	}
	if m, ok := value.AsMap(v); ok {
		return e.addSpec(strings.TrimSpace(name), m)
	}
	return e.addValue(strings.TrimSpace(name), "", "", v)
}

func (e *exec) multipartFiles(expr string) error {
	v, err := e.eval(expr)
	if err != nil {
		return err
	}
	if items, ok := value.AsSlice(v); ok {
		for _, item := range items {
			m, ok := value.AsMap(item)
			if !ok {
				return errors.New("multipart files entry expects an object")
			}
			name := strings.TrimSpace(value.Sprint(m["name"]))
			if name == "" {
				return errors.New("multipart files entry requires name")
			}
			if err := e.addSpec(name, m); err != nil {
				return err
			}
		}
		return nil
	}
	m, ok := value.AsMap(v)
	if !ok {
		return errors.New("multipart files expects a list or an object")
	}
	for k, child := range m {
		spec, ok := value.AsMap(child)
		if !ok {
			return errors.New("multipart files entry expects an object")
		}
		if err := e.addSpec(k, spec); err != nil {
			return err
		}
	}
	return nil
}

func partSpec(m map[string]any) bool {
	for _, k := range []string{"value", "read", "contentType", "filename"} {
		if m[k] != nil {
			return true
		}
	}
	return false
}

func (e *exec) addSpec(name string, m map[string]any) error {
	filename, ctype := "", ""
	if m["filename"] != nil {
		filename = value.Sprint(m["filename"])
	}
	if m["contentType"] != nil {
		ctype = value.Sprint(m["contentType"])
	}
	if m["read"] != nil {
		ref := value.Sprint(m["read"])
		body, err := e.vm.Bytes(ref)
		if err != nil {
			return err
		}
		if filename == "" {
			filename = path.Base(ref)
		}
		if ctype == "" {
			ctype = typeByName(filename)
		}
		return e.add(name, filename, ctype, body)
	}
	if m["value"] != nil {
		return e.addValue(name, filename, ctype, m["value"])
	}
	return fmt.Errorf("multipart %s has no read or value", name)
}

func (e *exec) addValue(name, filename, ctype string, v any) error {
	body, kind, err := partBody(v)
	if err != nil {
		return err
	}
	if ctype == "" {
		ctype = kind
	}
	return e.add(name, filename, ctype, body)
}

func (e *exec) add(name, filename, ctype string, body []byte) error {
	if name == "" {
		return errors.New("multipart part requires a name")
	}
	e.sess.parts = append(e.sess.parts, part{name: name, filename: filename, ctype: ctype, body: body})
	return nil
}

func partBody(v any) ([]byte, string, error) {
	if v == nil {
		return nil, "text/plain", nil
	}
	if _, ok := value.AsMap(v); ok {
		b, err := value.Marshal(v)
		return b, "application/json", err
	}
	if _, ok := value.AsSlice(v); ok {
		b, err := value.Marshal(v)
		return b, "application/json", err
	}
	return []byte(value.Sprint(v)), "text/plain", nil
}

func typeByName(name string) string {
	if t := mimetype(name); t != "" {
		return t
	}
	return "application/octet-stream"
}

func mimetype(name string) string {
	t := mime.TypeByExtension(path.Ext(name))
	if i := strings.IndexByte(t, ';'); i >= 0 {
		t = t[:i]
	}
	return strings.TrimSpace(t)
}

func encodeParts(parts []part) ([]byte, string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for _, p := range parts {
		h := make(textproto.MIMEHeader)
		disp := `form-data; name="` + escapeQuotes(p.name) + `"`
		if p.filename != "" {
			disp += `; filename="` + escapeQuotes(p.filename) + `"`
		}
		h.Set("Content-Disposition", disp)
		if p.ctype != "" {
			h.Set("Content-Type", p.ctype)
		}
		wr, err := w.CreatePart(h)
		if err != nil {
			return nil, "", err
		}
		if _, err := wr.Write(p.body); err != nil {
			return nil, "", err
		}
	}
	if err := w.Close(); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), w.FormDataContentType(), nil
}

func escapeQuotes(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s)
}
