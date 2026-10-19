package feature

import (
	"errors"
	"fmt"
	"strings"
)

// Selected reports whether tags satisfy a tag expression.
// A bare name matches that tag. Commas and "or" match any. "and" matches all.
// "not" and a leading "~" negate. anyOf() and allOf() are the same two operations.
func Selected(tags []string, expr string) (bool, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return true, nil
	}
	p := &sel{has: tagSet(tags), s: expr}
	ok, err := p.or()
	if err != nil {
		return false, err
	}
	p.skip()
	if p.i < len(p.s) {
		return false, fmt.Errorf("tag: unexpected %q", strings.TrimSpace(p.s[p.i:]))
	}
	return ok, nil
}

func tagSet(tags []string) map[string]bool {
	m := make(map[string]bool, len(tags))
	for _, t := range tags {
		m[strings.TrimPrefix(t, "@")] = true
	}
	return m
}

type sel struct {
	has map[string]bool
	s   string
	i   int
}

func (p *sel) or() (bool, error) {
	left, err := p.and()
	if err != nil {
		return false, err
	}
	for p.word("or") || p.sym("||") || p.sym(",") {
		right, err := p.and()
		if err != nil {
			return false, err
		}
		left = left || right
	}
	return left, nil
}

func (p *sel) and() (bool, error) {
	left, err := p.unary()
	if err != nil {
		return false, err
	}
	for p.word("and") || p.sym("&&") {
		right, err := p.unary()
		if err != nil {
			return false, err
		}
		left = left && right
	}
	return left, nil
}

func (p *sel) unary() (bool, error) {
	if p.word("not") || p.sym("~") {
		v, err := p.unary()
		if err != nil {
			return false, err
		}
		return !v, nil
	}
	return p.primary()
}

func (p *sel) primary() (bool, error) {
	p.skip()
	if p.i < len(p.s) && p.s[p.i] == '(' {
		p.i++
		v, err := p.or()
		if err != nil {
			return false, err
		}
		if !p.sym(")") {
			return false, errors.New("tag: missing )")
		}
		return v, nil
	}
	name, err := p.tag()
	if err != nil {
		return false, err
	}
	p.skip()
	if (name == "anyOf" || name == "allOf") && p.i < len(p.s) && p.s[p.i] == '(' {
		args, err := p.args()
		if err != nil {
			return false, err
		}
		if name == "anyOf" {
			for _, a := range args {
				if p.has[a] {
					return true, nil
				}
			}
			return false, nil
		}
		for _, a := range args {
			if !p.has[a] {
				return false, nil
			}
		}
		return len(args) > 0, nil
	}
	return p.has[name], nil
}

func (p *sel) args() ([]string, error) {
	if !p.sym("(") {
		return nil, errors.New("tag: expected (")
	}
	var out []string
	for {
		p.skip()
		if p.sym(")") {
			return out, nil
		}
		if len(out) > 0 && !p.sym(",") {
			return nil, errors.New("tag: expected , or )")
		}
		name, err := p.tag()
		if err != nil {
			return nil, err
		}
		out = append(out, name)
	}
}

func (p *sel) tag() (string, error) {
	p.skip()
	if p.i >= len(p.s) {
		return "", errors.New("tag: expected a tag")
	}
	if p.s[p.i] == '\'' || p.s[p.i] == '"' {
		q := p.s[p.i]
		p.i++
		start := p.i
		for p.i < len(p.s) && p.s[p.i] != q {
			p.i++
		}
		if p.i >= len(p.s) {
			return "", errors.New("tag: unclosed quote")
		}
		t := p.s[start:p.i]
		p.i++
		return strings.TrimPrefix(t, "@"), nil
	}
	if p.s[p.i] == '@' {
		p.i++
	}
	start := p.i
	for p.i < len(p.s) && tagChar(p.s[p.i]) {
		p.i++
	}
	if start == p.i {
		return "", errors.New("tag: expected a tag")
	}
	return p.s[start:p.i], nil
}

func tagChar(c byte) bool {
	return c == '=' || c == ':' || c == '/' || c == '_' || c == '-' || c == '.' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

func (p *sel) skip() {
	for p.i < len(p.s) && (p.s[p.i] == ' ' || p.s[p.i] == '\t') {
		p.i++
	}
}

func (p *sel) word(w string) bool {
	p.skip()
	if !strings.HasPrefix(p.s[p.i:], w) {
		return false
	}
	end := p.i + len(w)
	if end < len(p.s) && tagChar(p.s[end]) {
		return false
	}
	p.i = end
	return true
}

func (p *sel) sym(s string) bool {
	p.skip()
	if !strings.HasPrefix(p.s[p.i:], s) {
		return false
	}
	p.i += len(s)
	return true
}
