package xml

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode"

	"github.com/gi-runner/gi/internal/value"
)

func Parse(raw string) (any, error) {
	dec := xml.NewDecoder(strings.NewReader(raw))
	dec.CharsetReader = func(string, io.Reader) (io.Reader, error) {
		return nil, errors.New("xml charset is not utf-8")
	}
	root, err := readElem(dec)
	if err != nil {
		return nil, err
	}
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement, xml.EndElement:
			return nil, errors.New("XML must contain exactly one root element")
		case xml.CharData:
			if strings.TrimSpace(string(t)) != "" {
				return nil, errors.New("unexpected text after XML root")
			}
		}
	}
	return map[string]any{root.name: nodeValue(root)}, nil
}

type node struct {
	name  string
	attrs []xml.Attr
	text  string
	kids  []*node
}

func readElem(dec *xml.Decoder) (*node, error) {
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		if text, ok := tok.(xml.CharData); ok && strings.TrimSpace(string(text)) != "" {
			return nil, errors.New("unexpected text before XML root")
		}
		start, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		return readBody(dec, start)
	}
}

func readBody(dec *xml.Decoder, start xml.StartElement) (*node, error) {
	n := &node{name: start.Name.Local, attrs: start.Attr}
	var text strings.Builder
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			child, err := readBody(dec, t)
			if err != nil {
				return nil, err
			}
			n.kids = append(n.kids, child)
		case xml.CharData:
			text.Write(t)
		case xml.EndElement:
			n.text = strings.TrimSpace(text.String())
			return n, nil
		}
	}
}

func nodeValue(n *node) any {
	if len(n.attrs) == 0 && len(n.kids) == 0 {
		return n.text
	}
	m := map[string]any{}
	for _, a := range n.attrs {
		m["@"+a.Name.Local] = a.Value
	}
	if n.text != "" {
		m["_"] = n.text
	}
	grouped := map[string][]any{}
	var order []string
	for _, c := range n.kids {
		if _, ok := grouped[c.name]; !ok {
			order = append(order, c.name)
		}
		grouped[c.name] = append(grouped[c.name], nodeValue(c))
	}
	for _, name := range order {
		vs := grouped[name]
		if len(vs) == 1 {
			m[name] = vs[0]
		} else {
			m[name] = vs
		}
	}
	return m
}

func FromMap(m map[string]any) (*Document, error) {
	raw, err := Format(m)
	if err != nil {
		return nil, err
	}
	return ParseDocument(raw)
}

func Format(v any) (string, error) {
	if doc, ok := v.(*Document); ok {
		return doc.String(), nil
	}
	m, ok := value.AsMap(v)
	if !ok || len(m) != 1 {
		return "", errors.New("xml expects an object with one root")
	}
	var b strings.Builder
	for name, child := range m {
		if err := writeElem(&b, name, child); err != nil {
			return "", err
		}
	}
	return b.String(), nil
}

func writeElem(b *strings.Builder, name string, v any) error {
	if !validName(name) {
		return fmt.Errorf("invalid XML element name %q", name)
	}
	if items, ok := value.AsSlice(v); ok {
		for _, item := range items {
			if err := writeElem(b, name, item); err != nil {
				return err
			}
		}
		return nil
	}
	b.WriteByte('<')
	b.WriteString(name)
	var text string
	var attrs []string
	var kids [][2]any
	if m, ok := value.AsMap(v); ok {
		for k, child := range m {
			if strings.HasPrefix(k, "@") {
				attrs = append(attrs, k)
				continue
			}
			if k == "_" {
				text = value.Sprint(child)
				continue
			}
			kids = append(kids, [2]any{k, child})
		}
		sort.Strings(attrs)
		sort.Slice(kids, func(i, j int) bool {
			left, _ := kids[i][0].(string)
			right, _ := kids[j][0].(string)
			return left < right
		})
		for _, k := range attrs {
			if !validName(strings.TrimPrefix(k, "@")) {
				return fmt.Errorf("invalid XML attribute name %q", k)
			}
			b.WriteByte(' ')
			b.WriteString(strings.TrimPrefix(k, "@"))
			b.WriteString(`="`)
			if err := escape(b, value.Sprint(m[k])); err != nil {
				return err
			}
			b.WriteByte('"')
		}
	} else if v != nil {
		text = value.Sprint(v)
	}
	if text == "" && len(kids) == 0 {
		b.WriteString("/>")
		return nil
	}
	b.WriteByte('>')
	if text != "" {
		if err := escape(b, text); err != nil {
			return err
		}
	}
	for _, kid := range kids {
		name, ok := kid[0].(string)
		if !ok {
			return fmt.Errorf("xml child name is %T", kid[0])
		}
		if err := writeElem(b, name, kid[1]); err != nil {
			return err
		}
	}
	b.WriteString("</")
	b.WriteString(name)
	b.WriteByte('>')
	return nil
}

func escape(b *strings.Builder, s string) error {
	var buf bytes.Buffer
	if err := xml.EscapeText(&buf, []byte(s)); err != nil {
		return err
	}
	b.Write(buf.Bytes())
	return nil
}

func validName(name string) bool {
	parts := strings.Split(name, ":")
	if len(parts) > 2 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		for i, r := range part {
			if unicode.IsLetter(r) || r == '_' {
				continue
			}
			if i > 0 && (unicode.IsDigit(r) || unicode.IsMark(r) || r == '-' || r == '.' || r == '\u00b7') {
				continue
			}
			return false
		}
	}
	return true
}
