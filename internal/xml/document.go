package xml

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/antchfx/xmlquery"
	"github.com/antchfx/xpath"
	"github.com/gi-runner/gi/internal/value"
)

// Document retains namespaces and mixed-content order independently of its
// convenient JSON-shaped read view. A document belongs to one scenario.
type Document struct{ root *xmlquery.Node }

func ParseDocument(raw string) (*Document, error) {
	if _, err := Parse(raw); err != nil {
		return nil, err
	}
	root, err := xmlquery.Parse(strings.NewReader(raw))
	if err != nil {
		return nil, err
	}
	return &Document{root: root}, nil
}

func (d *Document) String() string {
	return d.root.OutputXMLWithOptions(xmlquery.WithPreserveSpace(), xmlquery.WithEmptyTagSupport())
}
func (d *Document) MarshalJSON() ([]byte, error) { return json.Marshal(d.DataValue()) }
func (d *Document) CloneValue() (any, error)     { return &Document{root: cloneNode(d.root)}, nil }
func (d *Document) DataValue() any {
	for n := d.root.FirstChild; n != nil; n = n.NextSibling {
		if n.Type == xmlquery.ElementNode {
			return map[string]any{n.Data: nodeModel(n)}
		}
	}
	return map[string]any{}
}

func nodeModel(n *xmlquery.Node) any {
	m := make(map[string]any)
	for _, a := range n.Attr {
		m["@"+a.Name.Local] = a.Value
	}
	var text strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		switch c.Type {
		case xmlquery.TextNode, xmlquery.CharDataNode:
			text.WriteString(c.Data)
		case xmlquery.ElementNode:
			v := nodeModel(c)
			if previous, exists := m[c.Data]; exists {
				if list, ok := previous.([]any); ok {
					m[c.Data] = append(list, v)
				} else {
					m[c.Data] = []any{previous, v}
				}
			} else {
				m[c.Data] = v
			}
		case xmlquery.DocumentNode, xmlquery.DeclarationNode, xmlquery.CommentNode, xmlquery.AttributeNode, xmlquery.NotationNode, xmlquery.ProcessingInstruction:
		}
	}
	trimmed := strings.TrimSpace(text.String())
	if len(m) == 0 {
		return trimmed
	}
	if trimmed != "" {
		m["_"] = trimmed
	}
	return m
}

func cloneNode(n *xmlquery.Node) *xmlquery.Node {
	out := &xmlquery.Node{Type: n.Type, Data: n.Data, Prefix: n.Prefix, NamespaceURI: n.NamespaceURI, Attr: append([]xmlquery.Attr(nil), n.Attr...)}
	if n.ProcInst != nil {
		p := *n.ProcInst
		out.ProcInst = &p
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		xmlquery.AddChild(out, cloneNode(c))
	}
	return out
}

func (d *Document) namespaces() map[string]string {
	ns := map[string]string{"xml": "http://www.w3.org/XML/1998/namespace"}
	var visit func(*xmlquery.Node)
	visit = func(n *xmlquery.Node) {
		if n.Prefix != "" && n.NamespaceURI != "" {
			if _, exists := ns[n.Prefix]; !exists {
				ns[n.Prefix] = n.NamespaceURI
			}
		}
		for _, a := range n.Attr {
			if a.Name.Space == "xmlns" {
				if _, exists := ns[a.Name.Local]; !exists {
					ns[a.Name.Local] = a.Value
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			visit(c)
		}
	}
	visit(d.root)
	return ns
}

func (d *Document) expression(path string) (*xpath.Expr, error) {
	expr, err := xpath.CompileWithNS(path, d.namespaces())
	if err != nil {
		return nil, fmt.Errorf("XPath %q: %w", path, err)
	}
	return expr, nil
}

func (d *Document) Query(path string) (any, error) {
	expr, err := d.expression(path)
	if err != nil {
		return nil, err
	}
	result := expr.Evaluate(xmlquery.CreateXPathNavigator(d.root))
	iterator, ok := result.(*xpath.NodeIterator)
	if !ok {
		return value.Normalize(result), nil
	}
	out := make([]any, 0)
	for iterator.MoveNext() {
		nav, ok := iterator.Current().(*xmlquery.NodeNavigator)
		if !ok {
			return nil, errors.New("unexpected XPath navigator")
		}
		if nav.NodeType() == xpath.AttributeNode || nav.NodeType() == xpath.TextNode {
			out = append(out, nav.Value())
			continue
		}
		if nav.NodeType() == xpath.RootNode {
			copied, err := d.CloneValue()
			if err != nil {
				return nil, err
			}
			out = append(out, copied)
			continue
		}
		n := nav.Current()
		leaf := true
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == xmlquery.ElementNode {
				leaf = false
				break
			}
		}
		if leaf {
			out = append(out, n.InnerText())
		} else {
			doc := &xmlquery.Node{Type: xmlquery.DocumentNode}
			xmlquery.AddChild(doc, cloneNode(n))
			// Make detached fragments self-contained when selected below a namespace declaration.
			for prefix, uri := range namespaceBindings(n) {
				name := "xmlns"
				if prefix != "" {
					name += ":" + prefix
				}
				if !doc.FirstChild.HasAttr(name) {
					doc.FirstChild.SetAttr(name, uri)
				}
			}
			out = append(out, &Document{root: doc})
		}
	}
	if len(out) == 0 {
		return value.Absent{}, nil
	}
	if len(out) == 1 {
		return out[0], nil
	}
	return out, nil
}

// Comparable includes expanded namespace names and ordered content. Namespace
// prefix spelling and formatting whitespace do not change element identity.
func (d *Document) Comparable() any {
	var canonical func(*xmlquery.Node) any
	canonical = func(n *xmlquery.Node) any {
		attrs := make(map[string]any)
		for _, a := range n.Attr {
			if a.Name.Space == "xmlns" || a.Name.Local == "xmlns" {
				continue
			}
			attrs["{"+a.NamespaceURI+"}"+a.Name.Local] = a.Value
		}
		content := make([]any, 0)
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			switch c.Type {
			case xmlquery.ElementNode:
				content = append(content, canonical(c))
			case xmlquery.TextNode, xmlquery.CharDataNode:
				if strings.TrimSpace(c.Data) != "" {
					content = append(content, c.Data)
				}
			case xmlquery.DocumentNode, xmlquery.DeclarationNode, xmlquery.CommentNode, xmlquery.AttributeNode, xmlquery.NotationNode, xmlquery.ProcessingInstruction:
			}
		}
		return map[string]any{"name": "{" + n.NamespaceURI + "}" + n.Data, "attributes": attrs, "content": content}
	}
	for n := d.root.FirstChild; n != nil; n = n.NextSibling {
		if n.Type == xmlquery.ElementNode {
			return canonical(n)
		}
	}
	return nil
}

func (d *Document) Set(path string, val any) error { return d.mutate(path, val, false) }
func (d *Document) Remove(path string) error       { return d.mutate(path, nil, true) }
func (d *Document) mutate(path string, val any, remove bool) error {
	expr, err := d.expression(path)
	if err != nil {
		return err
	}
	type selectedNode struct {
		node      *xmlquery.Node
		attribute string
	}
	nodes := make([]selectedNode, 0)
	iterator := expr.Select(xmlquery.CreateXPathNavigator(d.root))
	for iterator.MoveNext() {
		nav, ok := iterator.Current().(*xmlquery.NodeNavigator)
		if !ok {
			return errors.New("unexpected XPath navigator")
		}
		item := selectedNode{node: nav.Current()}
		if nav.NodeType() == xpath.AttributeNode {
			item.attribute = nav.LocalName()
			if nav.Prefix() != "" {
				item.attribute = nav.Prefix() + ":" + item.attribute
			}
		}
		nodes = append(nodes, item)
	}
	if len(nodes) == 0 {
		if remove {
			return nil
		}
		parentPath, name, found := strings.LastIndex(path, "/"), "", false
		if parentPath >= 0 {
			name = path[parentPath+1:]
			found = name != "" && !strings.ContainsAny(name, "[]()*")
		}
		if !found {
			return fmt.Errorf("XPath %q selected no nodes", path)
		}
		parentExpr := path[:parentPath]
		parents, err := d.expression(parentExpr)
		if err != nil {
			return err
		}
		selected := xmlquery.QuerySelectorAll(d.root, parents)
		if len(selected) == 0 {
			parent, err := d.ensureElement(parentExpr)
			if err != nil {
				return err
			}
			selected = []*xmlquery.Node{parent}
		}
		for _, p := range selected {
			if attr, ok := strings.CutPrefix(name, "@"); ok {
				p.SetAttr(attr, value.Sprint(val))
				continue
			}
			n, err := d.newElement(p, name)
			if err != nil {
				return err
			}
			xmlquery.AddChild(p, n)
			if err := setNode(n, val); err != nil {
				return err
			}
		}
		return nil
	}
	for _, selected := range nodes {
		n := selected.node
		if selected.attribute != "" {
			if remove {
				n.RemoveAttr(selected.attribute)
			} else {
				n.SetAttr(selected.attribute, value.Sprint(val))
			}
		} else if remove {
			xmlquery.RemoveFromTree(n)
		} else if err := setNode(n, val); err != nil {
			return err
		}
	}
	return nil
}

func setNode(n *xmlquery.Node, val any) error {
	if doc, ok := val.(*Document); ok {
		for c := doc.root.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == xmlquery.ElementNode {
				replacement := cloneNode(c)
				if n.Parent == nil {
					return errors.New("cannot replace an XML document root")
				}
				xmlquery.AddImmediateSibling(n, replacement)
				xmlquery.RemoveFromTree(n)
				return nil
			}
		}
	}
	for c := n.FirstChild; c != nil; {
		next := c.NextSibling
		xmlquery.RemoveFromTree(c)
		c = next
	}
	if n.Type == xmlquery.TextNode || n.Type == xmlquery.CharDataNode {
		n.Data = value.Sprint(val)
		return nil
	}
	if val != nil {
		xmlquery.AddChild(n, &xmlquery.Node{Type: xmlquery.TextNode, Data: value.Sprint(val)})
	}
	return nil
}

func (d *Document) newElement(parent *xmlquery.Node, name string) (*xmlquery.Node, error) {
	if !validName(name) {
		return nil, fmt.Errorf("invalid XML element name %q", name)
	}
	prefix, local, qualified := strings.Cut(name, ":")
	if !qualified {
		local, prefix = name, ""
	}
	namespace := ""
	for p := parent; p != nil; p = p.Parent {
		for _, a := range p.Attr {
			if (prefix == "" && a.Name.Local == "xmlns" && a.Name.Space == "") || (a.Name.Space == "xmlns" && a.Name.Local == prefix) {
				namespace = a.Value
				break
			}
		}
		if namespace != "" {
			break
		}
		if p.Prefix == prefix && p.NamespaceURI != "" {
			namespace = p.NamespaceURI
			break
		}
	}
	if prefix != "" && namespace == "" {
		return nil, fmt.Errorf("unbound XML namespace prefix %q", prefix)
	}
	return &xmlquery.Node{Type: xmlquery.ElementNode, Data: local, Prefix: prefix, NamespaceURI: namespace}, nil
}
func (d *Document) ensureElement(path string) (*xmlquery.Node, error) {
	if !strings.HasPrefix(path, "/") || strings.ContainsAny(path, "[]()*@") || strings.Contains(path, "//") {
		return nil, fmt.Errorf("cannot create missing XML parent %q", path)
	}
	current := d.root
	for name := range strings.SplitSeq(strings.TrimPrefix(path, "/"), "/") {
		if !validName(name) {
			return nil, fmt.Errorf("invalid XML path component %q", name)
		}
		var found *xmlquery.Node
		for c := current.FirstChild; c != nil; c = c.NextSibling {
			qualified := c.Data
			if c.Prefix != "" {
				qualified = c.Prefix + ":" + qualified
			}
			if c.Type == xmlquery.ElementNode && qualified == name {
				found = c
				break
			}
		}
		if found == nil {
			if current == d.root {
				return nil, errors.New("XML root does not match the requested path")
			}
			created, err := d.newElement(current, name)
			if err != nil {
				return nil, err
			}
			xmlquery.AddChild(current, created)
			found = created
		}
		current = found
	}
	return current, nil
}

func namespaceBindings(node *xmlquery.Node) map[string]string {
	bindings := make(map[string]string)
	for current := node; current != nil; current = current.Parent {
		for _, attr := range current.Attr {
			prefix := ""
			if attr.Name.Space == "xmlns" {
				prefix = attr.Name.Local
			} else if attr.Name.Local != "xmlns" || attr.Name.Space != "" {
				continue
			}
			if _, exists := bindings[prefix]; !exists {
				bindings[prefix] = attr.Value
			}
		}
	}
	return bindings
}
