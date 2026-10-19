package engine

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/gi-runner/gi/internal/value"
)

func (e *exec) assign(name string, v any) error {
	name = strings.TrimSpace(name)
	if !ident(name) {
		return errors.New("name must be an identifier")
	}
	e.vm.Set(name, v)
	e.defined[name] = true
	return nil
}

func (e *exec) textStep(rest, doc string) error {
	name, rhs, ok := splitAssign(rest)
	if !ok {
		return errors.New("text expects name = value")
	}
	if strings.TrimSpace(doc) != "" {
		rhs = doc
	} else {
		rhs = strings.TrimSpace(rhs)
	}
	return e.assign(name, rhs)
}

func (e *exec) jsonStep(rest, doc string) error {
	name, rhs, ok := splitAssign(rest)
	if !ok {
		return errors.New("json expects name = value")
	}
	if strings.TrimSpace(rhs) == "" {
		rhs = doc
	}
	v, err := e.eval(rhs)
	if err != nil {
		return err
	}
	if doc, ok := v.(interface{ DataValue() any }); ok {
		v = doc.DataValue()
	}
	if s, ok := v.(string); ok {
		v, err = value.Parse([]byte(s))
		if err != nil {
			return fmt.Errorf("json: %w", err)
		}
	}
	return e.assign(name, v)
}

func (e *exec) copyStep(rest string) error {
	name, rhs, ok := splitAssign(rest)
	if !ok {
		return errors.New("copy expects name = value")
	}
	v, err := e.eval(rhs)
	if err != nil {
		return err
	}
	v, err = value.Clone(v)
	if err != nil {
		return err
	}
	return e.assign(name, v)
}

func (e *exec) tableStep(name string, rows [][]string) error {
	if len(rows) == 0 {
		return errors.New("table requires a data table")
	}
	header := rows[0]
	out := make([]any, 0, len(rows)-1)
	for _, row := range rows[1:] {
		obj := map[string]any{}
		for i, cell := range row {
			if i >= len(header) || header[i] == "" || cell == "" {
				continue
			}
			keepNull := len(cell) >= 2 && strings.HasPrefix(cell, "(") && strings.HasSuffix(cell, ")")
			v, err := e.eval(cell)
			if err != nil {
				return err
			}
			if v == nil && !keepNull {
				continue
			}
			obj[header[i]] = v
		}
		out = append(out, obj)
	}
	return e.assign(name, out)
}

func (e *exec) replaceStep(rest string, rows [][]string) error {
	if len(rows) > 0 {
		return e.replaceRows(strings.TrimSpace(rest), rows)
	}
	lhs, rhs, ok := splitAssign(rest)
	if !ok {
		return errors.New("replace expects name.token = value")
	}
	varName, token, ok := strings.Cut(strings.TrimSpace(lhs), ".")
	if !ok || varName == "" || token == "" {
		return errors.New("replace expects name.token = value")
	}
	repl, err := e.eval(rhs)
	if err != nil {
		return err
	}
	return e.writeReplaced(varName, token, repl)
}

func (e *exec) replaceRows(name string, rows [][]string) error {
	tokenCol, valueCol := 0, 1
	if len(rows[0]) >= 2 {
		for i, h := range rows[0] {
			switch strings.ToLower(strings.TrimSpace(h)) {
			case "token":
				tokenCol = i
			case "value":
				valueCol = i
			}
		}
	}
	cur, ok := e.vm.Get(name)
	if !ok {
		return fmt.Errorf("no variable %s", name)
	}
	for _, row := range rows[1:] {
		if tokenCol >= len(row) || valueCol >= len(row) {
			return errors.New("replace row needs token and value")
		}
		repl, err := e.eval(row[valueCol])
		if err != nil {
			return err
		}
		next, err := replaceToken(cur, row[tokenCol], repl)
		if err != nil {
			return err
		}
		cur = next
	}
	return e.assign(name, cur)
}

func (e *exec) writeReplaced(name, token string, repl any) error {
	cur, ok := e.vm.Get(name)
	if !ok {
		return fmt.Errorf("no variable %s", name)
	}
	next, err := replaceToken(cur, token, repl)
	if err != nil {
		return err
	}
	return e.assign(name, next)
}

func replaceToken(cur any, token string, repl any) (string, error) {
	text, err := replaceText(cur)
	if err != nil {
		return "", err
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return "", errors.New("replace token is empty")
	}
	r, _ := utf8.DecodeRuneInString(token)
	if unicode.IsLetter(r) || unicode.IsDigit(r) {
		token = "<" + token + ">"
	}
	return strings.ReplaceAll(text, token, value.Sprint(repl)), nil
}

func replaceText(v any) (string, error) {
	switch t := v.(type) {
	case string:
		return t, nil
	case map[string]any, []any:
		b, err := value.Marshal(t)
		if err != nil {
			return "", err
		}
		return string(b), nil
	default:
		return value.Sprint(v), nil
	}
}

func (e *exec) assertStep(expr string) error {
	v, err := e.eval(expr)
	if err != nil {
		return err
	}
	ok, isBool := v.(bool)
	if !isBool {
		return errors.New("assert expression must be a boolean")
	}
	if !ok {
		return fmt.Errorf("assert failed: %s", strings.TrimSpace(expr))
	}
	return nil
}
