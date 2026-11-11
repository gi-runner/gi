package spec

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type operation struct {
	method, path, id, summary string
	params                    []parameter
	body                      *schema
	bodyRequired              bool
	bodyExample               any
	responses                 map[int]resp
	status                    int
	synthetic                 bool
	securitySet               bool
	security                  [][]string
	schemes                   map[string]secScheme
}

// secScheme is one OpenAPI security scheme the compiler can place on a request.
type secScheme struct {
	typ, in, name string
}

type parameter struct {
	name, in string
	required bool
	example  any
}

type resp struct {
	schema *schema
}

type schema struct {
	Ref        string            `yaml:"$ref"`
	Type       string            `yaml:"type"`
	Format     string            `yaml:"format"`
	Required   []string          `yaml:"required"`
	Properties map[string]schema `yaml:"properties"`
	Items      *schema           `yaml:"items"`
	Enum       []any             `yaml:"enum"`
	Example    any               `yaml:"example"`
	AllOf      []schema          `yaml:"allOf"`
}

type contract struct {
	schemas map[string]schema
}

type oas struct {
	Security   yaml.Node           `yaml:"security"`
	Paths      map[string]pathItem `yaml:"paths"`
	Components struct {
		Schemas         map[string]schema    `yaml:"schemas"`
		SecuritySchemes map[string]oasScheme `yaml:"securitySchemes"`
	} `yaml:"components"`
}

type oasScheme struct {
	Type string `yaml:"type"`
	In   string `yaml:"in"`
	Name string `yaml:"name"`
}

type pathItem struct {
	Parameters []oasParam `yaml:"parameters"`
	Get        *oasOp     `yaml:"get"`
	Post       *oasOp     `yaml:"post"`
	Put        *oasOp     `yaml:"put"`
	Patch      *oasOp     `yaml:"patch"`
	Delete     *oasOp     `yaml:"delete"`
	Head       *oasOp     `yaml:"head"`
	Options    *oasOp     `yaml:"options"`
}

type oasParam struct {
	Name     string  `yaml:"name"`
	In       string  `yaml:"in"`
	Required bool    `yaml:"required"`
	Example  any     `yaml:"example"`
	Schema   *schema `yaml:"schema"`
}

type oasOp struct {
	OperationID string     `yaml:"operationId"`
	Summary     string     `yaml:"summary"`
	Security    yaml.Node  `yaml:"security"`
	Parameters  []oasParam `yaml:"parameters"`
	RequestBody *struct {
		Required bool                `yaml:"required"`
		Content  map[string]oasMedia `yaml:"content"`
	} `yaml:"requestBody"`
	Responses yaml.Node `yaml:"responses"`
}

type oasMedia struct {
	Schema  *schema `yaml:"schema"`
	Example any     `yaml:"example"`
}

func readContracts(dir string) ([]operation, error) {
	paths, err := contractPaths(dir)
	if err != nil {
		return nil, err
	}
	var ops []operation
	seen := map[string]bool{}
	for _, path := range paths {
		text, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var doc oas
		if err := yaml.Unmarshal(text, &doc); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		c := contract{schemas: doc.Components.Schemas}
		for _, op := range c.operations(doc) {
			key := op.method + " " + op.path
			if seen[key] {
				continue
			}
			seen[key] = true
			ops = append(ops, op)
		}
	}
	slices.SortFunc(ops, func(a, b operation) int {
		if c := strings.Compare(a.path, b.path); c != 0 {
			return c
		}
		return strings.Compare(a.method, b.method)
	})
	return ops, nil
}

func contractPaths(dir string) ([]string, error) {
	var paths []string
	for _, name := range []string{"openapi.yaml", "openapi.yml", "openapi.json"} {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err == nil {
			paths = append(paths, path)
		}
	}
	contracts := filepath.Join(dir, "contracts")
	entries, err := os.ReadDir(contracts)
	if err != nil {
		if os.IsNotExist(err) {
			slices.Sort(paths)
			return paths, nil
		}
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		switch strings.ToLower(filepath.Ext(entry.Name())) {
		case ".yaml", ".yml", ".json":
			paths = append(paths, filepath.Join(contracts, entry.Name()))
		}
	}
	slices.Sort(paths)
	return paths, nil
}

func (c contract) operations(doc oas) []operation {
	schemes := schemesFrom(doc.Components.SecuritySchemes)
	var ops []operation
	for path, item := range doc.Paths {
		for _, pair := range []struct {
			method string
			op     *oasOp
		}{
			{"get", item.Get}, {"post", item.Post}, {"put", item.Put}, {"patch", item.Patch},
			{"delete", item.Delete}, {"head", item.Head}, {"options", item.Options},
		} {
			if pair.op == nil {
				continue
			}
			ops = append(ops, c.operation(pair.method, path, item.Parameters, pair.op, doc.Security, schemes))
		}
	}
	return ops
}

func schemesFrom(raw map[string]oasScheme) map[string]secScheme {
	out := make(map[string]secScheme, len(raw))
	for name, scheme := range raw {
		out[name] = secScheme{typ: scheme.Type, in: scheme.In, name: scheme.Name}
	}
	return out
}

func decodeSecurity(node yaml.Node) ([][]string, bool) {
	if node.Kind == 0 || node.Tag == "!!null" {
		return nil, false
	}
	var raw []map[string][]string
	if err := node.Decode(&raw); err != nil {
		return [][]string{{"security"}}, true
	}
	out := make([][]string, 0, len(raw))
	for _, alt := range raw {
		names := make([]string, 0, len(alt))
		for name := range alt {
			names = append(names, name)
		}
		slices.Sort(names)
		out = append(out, names)
	}
	return out, true
}

func (c contract) operation(method, path string, shared []oasParam, raw *oasOp, global yaml.Node, schemes map[string]secScheme) operation {
	op := operation{method: method, path: path, id: raw.OperationID, summary: raw.Summary, responses: map[int]resp{}, schemes: schemes}
	node := global
	if raw.Security.Kind != 0 {
		node = raw.Security
	}
	if reqs, ok := decodeSecurity(node); ok {
		op.securitySet = true
		op.security = reqs
	}
	params := append(append([]oasParam{}, shared...), raw.Parameters...)
	for _, p := range params {
		op.params = append(op.params, c.param(p))
	}
	if raw.RequestBody != nil {
		op.bodyRequired = raw.RequestBody.Required
		if media, ok := jsonMedia(raw.RequestBody.Content); ok {
			op.bodyExample = media.Example
			if media.Schema != nil {
				concrete := c.concrete(*media.Schema)
				op.body = &concrete
				if op.bodyExample == nil {
					op.bodyExample = concrete.Example
				}
			}
		}
	}
	for code, body := range decodeResponses(raw.Responses) {
		n, err := strconv.Atoi(code)
		if err != nil {
			continue
		}
		var parsed resp
		if body.Schema != nil {
			concrete := c.concrete(*body.Schema)
			parsed.schema = &concrete
		}
		op.responses[n] = parsed
		if n >= 200 && n < 300 && (op.status == 0 || n < op.status) {
			op.status = n
		}
	}
	if op.status == 0 {
		op.status = defaultStatus(method)
	}
	return op
}

func (c contract) param(p oasParam) parameter {
	out := parameter{name: p.Name, in: p.In, required: p.Required || p.In == "path", example: p.Example}
	if p.Schema != nil {
		concrete := c.concrete(*p.Schema)
		if out.example == nil {
			out.example = concrete.Example
		}
		if out.example == nil && len(concrete.Enum) > 0 {
			out.example = concrete.Enum[0]
		}
	}
	return out
}

func (c contract) concrete(s schema) schema {
	return c.walk(s, map[string]bool{})
}

func (c contract) walk(s schema, seen map[string]bool) schema {
	if s.Ref != "" {
		name, ok := strings.CutPrefix(s.Ref, "#/components/schemas/")
		if !ok || seen[name] {
			return schema{Type: "object"}
		}
		target, ok := c.schemas[name]
		if !ok {
			return schema{Type: "object"}
		}
		seen[name] = true
		out := c.walk(target, seen)
		delete(seen, name)
		if s.Example != nil {
			out.Example = s.Example
		}
		return out
	}
	if len(s.AllOf) > 0 {
		merged := schema{Type: "object", Properties: map[string]schema{}}
		for _, part := range s.AllOf {
			p := c.walk(part, seen)
			maps.Copy(merged.Properties, p.Properties)
			merged.Required = append(merged.Required, p.Required...)
			if p.Example != nil {
				merged.Example = p.Example
			}
		}
		return merged
	}
	if s.Items != nil {
		item := c.walk(*s.Items, seen)
		s.Items = &item
	}
	if len(s.Properties) > 0 {
		props := make(map[string]schema, len(s.Properties))
		for k, v := range s.Properties {
			props[k] = c.walk(v, seen)
		}
		s.Properties = props
	}
	return s
}

func jsonMedia(content map[string]oasMedia) (oasMedia, bool) {
	if content == nil {
		return oasMedia{}, false
	}
	if media, ok := content["application/json"]; ok {
		return media, true
	}
	var kinds []string
	for kind := range content {
		if strings.Contains(kind, "json") {
			kinds = append(kinds, kind)
		}
	}
	if len(kinds) == 0 {
		return oasMedia{}, false
	}
	slices.Sort(kinds)
	return content[kinds[0]], true
}

func decodeResponses(node yaml.Node) map[string]oasMedia {
	out := map[string]oasMedia{}
	if node.Kind != yaml.MappingNode {
		return out
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		var body struct {
			Content map[string]oasMedia `yaml:"content"`
		}
		if err := node.Content[i+1].Decode(&body); err != nil {
			continue
		}
		if media, ok := jsonMedia(body.Content); ok {
			out[node.Content[i].Value] = media
		} else {
			out[node.Content[i].Value] = oasMedia{}
		}
	}
	return out
}

func defaultStatus(method string) int {
	if method == "post" {
		return 201
	}
	return 200
}

func placeholder(kind, name string) any {
	switch kind {
	case "integer", "number":
		return 1
	case "boolean":
		return true
	case "array":
		return []any{}
	case "object":
		return map[string]any{}
	default:
		return name
	}
}
