package engine

import (
	"iter"
	"strings"
)

// topLevel yields operator positions outside literals and nested expressions.
// It scans syntax only: locating a DSL operator must never execute JavaScript.
func topLevel(s string) iter.Seq[int] {
	return func(yield func(int) bool) {
		depth := 0
		expressionStart := true
		trimmed := strings.TrimSpace(s)
		_, path, xpath := splitXMLPath(trimmed)
		if strings.HasPrefix(trimmed, "/") {
			path, xpath = trimmed, true
		}
		if strings.HasPrefix(path, "/*") && strings.Contains(path, "*/") {
			xpath = false
		}
		for i := 0; i < len(s); i++ {
			c := s[i]
			if c == '\'' || c == '"' || c == '`' {
				i = literalEnd(s, i)
				expressionStart = false
				continue
			}
			if c == '/' && i+1 < len(s) {
				if !xpath && i > 0 && strings.ContainsRune(" \t\r\n", rune(s[i-1])) && s[i+1] == '/' {
					return
				}
				if !xpath && s[i+1] == '*' {
					end := strings.Index(s[i+2:], "*/")
					if end < 0 {
						return
					}
					i += end + 3
					continue
				}
				if expressionStart {
					// A slash can also begin an XPath. Only skip a complete regex literal.
					end := regexEnd(s, i)
					if end > i {
						i = end
						expressionStart = false
						continue
					}
				}
			}
			switch c {
			case '(', '[', '{':
				depth++
				expressionStart = true
				continue
			case ')', ']', '}':
				if depth > 0 {
					depth--
				}
				expressionStart = false
				continue
			}
			if depth == 0 && !yield(i) {
				return
			}
			if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
				expressionStart = strings.ContainsRune("=!:?,;&|+-*%<>~", rune(c))
			}
		}
	}
}

func regexEnd(s string, start int) int {
	class := false
	for i := start + 1; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '[':
			class = true
		case ']':
			class = false
		case '\n', '\r':
			return start
		case '/':
			if !class {
				return i
			}
		}
	}
	return start
}

// Templates can nest inside interpolation expressions. Their operators belong
// to JavaScript, never to the surrounding match or assignment statement.
func literalEnd(s string, start int) int {
	quote := s[start]
	for i := start + 1; i < len(s); i++ {
		switch {
		case s[i] == '\\':
			i++
		case s[i] == quote:
			return i
		case quote == '`' && s[i] == '$' && i+1 < len(s) && s[i+1] == '{':
			i = interpolationEnd(s, i+1)
		}
	}
	return len(s)
}

func interpolationEnd(s string, start int) int {
	depth := 1
	expressionStart := true
	for i := start + 1; i < len(s); i++ {
		c := s[i]
		switch c {
		case '\'', '"', '`':
			i = literalEnd(s, i)
			expressionStart = false
			continue
		case '/':
			if i+1 < len(s) && s[i+1] == '*' {
				end := strings.Index(s[i+2:], "*/")
				if end < 0 {
					return len(s)
				}
				i += end + 3
				continue
			}
			if i+1 < len(s) && s[i+1] == '/' {
				end := strings.IndexByte(s[i+2:], '\n')
				if end < 0 {
					return len(s)
				}
				i += end + 2
				continue
			}
			if expressionStart {
				if end := regexEnd(s, i); end > i {
					i = end
					expressionStart = false
					continue
				}
			}
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
		if !strings.ContainsRune(" \t\r\n", rune(c)) {
			expressionStart = strings.ContainsRune("=!:?,;&|+-*%<>~({[", rune(c))
		}
	}
	return len(s)
}
