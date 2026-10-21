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
		for i := 0; i < len(s); i++ {
			c := s[i]
			if c == '\'' || c == '"' || c == '`' {
				quote := c
				for i++; i < len(s); i++ {
					if s[i] == '\\' {
						i++
						continue
					}
					if s[i] == quote {
						break
					}
				}
				expressionStart = false
				continue
			}
			if c == '/' && i+1 < len(s) {
				if i > 0 && strings.ContainsRune(" \t\r\n", rune(s[i-1])) && s[i+1] == '/' {
					return
				}
				if i > 0 && strings.ContainsRune(" \t\r\n", rune(s[i-1])) && s[i+1] == '*' {
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
