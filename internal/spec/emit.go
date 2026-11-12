package spec

import (
	"fmt"
	"maps"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

type genFile struct {
	slug, title, text string
	tags              []string
	scenarios         []genScene
	loadLines         []string
	load              bool
	count             int
	clarifies         []string
}

type genScene struct {
	tags  []string
	name  string
	where string
	lines []string
}

func render(f genFile) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# gi-spec: %s\n", f.slug)
	for _, line := range f.loadLines {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	writeTags(&b, f.tags)
	b.WriteString("Feature: ")
	b.WriteString(f.title)
	b.WriteString("\n\nBackground:\n  * url baseUrl\n")
	for _, sc := range f.scenarios {
		b.WriteByte('\n')
		if sc.where != "" {
			b.WriteString("# ")
			b.WriteString(sc.where)
			b.WriteByte('\n')
		}
		writeTags(&b, sc.tags)
		b.WriteString("Scenario: ")
		b.WriteString(sc.name)
		b.WriteByte('\n')
		for _, line := range sc.lines {
			b.WriteString("  ")
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func writeTags(b *strings.Builder, tags []string) {
	if len(tags) == 0 {
		return
	}
	for i, tag := range tags {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteByte('@')
		b.WriteString(tag)
	}
	b.WriteByte('\n')
}

func sceneLines(op operation) []string {
	var lines []string
	if step := pathStep(op); step != "" {
		lines = append(lines, step)
	}
	_, send := credentials(op)
	for _, p := range op.params {
		if p.example == nil || !send[p.in+"\x00"+p.name] {
			continue
		}
		switch p.in {
		case "query":
			if ident(p.name) {
				lines = append(lines, "* param "+p.name+" = "+literal(p.example))
			}
		case "header":
			lines = append(lines, "* header "+p.name+" = "+literal(p.example))
		case "cookie":
			lines = append(lines, "* cookie "+p.name+" = "+literal(p.example))
		}
	}
	if line, ok := requestLine(op); ok {
		lines = append(lines, line)
	}
	lines = append(lines, "* method "+op.method, fmt.Sprintf("* status %d", op.status))
	if line := matchLine(responseSchema(op)); line != "" {
		lines = append(lines, line)
	}
	return lines
}

func pathStep(op operation) string {
	var args []string
	for _, part := range splitPath(op.path) {
		name, ok := strings.CutPrefix(part, "{")
		if ok && strings.HasSuffix(name, "}") {
			name = strings.TrimSuffix(name, "}")
			args = append(args, literal(paramValue(op, name)))
			continue
		}
		args = append(args, quote(part))
	}
	if len(args) == 0 {
		return ""
	}
	return "* path " + strings.Join(args, ", ")
}

func paramValue(op operation, name string) any {
	for _, p := range op.params {
		if p.in == "path" && p.name == name {
			return p.example
		}
	}
	return name
}

func requestLine(op operation) (string, bool) {
	if op.bodyExample != nil {
		return "* request " + literal(op.bodyExample), true
	}
	if !op.bodyRequired || op.body == nil {
		return "", false
	}
	return "* request " + literal(sample("", *op.body)), true
}

func responseSchema(op operation) *schema {
	if body, ok := op.responses[op.status]; ok {
		return body.schema
	}
	return nil
}

func matchLine(s *schema) string {
	if s == nil {
		return ""
	}
	switch {
	case s.Type == "array" && s.Items != nil && (s.Items.Type == "object" || len(s.Items.Properties) > 0):
		inner := objectShape(*s.Items, 0)
		if strings.HasPrefix(inner, "{") {
			return "* match each response contains " + inner
		}
	case s.Type == "object" || len(s.Properties) > 0:
		inner := objectShape(*s, 0)
		if strings.HasPrefix(inner, "{") {
			return "* match response contains " + inner
		}
	}
	expr := shape(*s, false, 0)
	if expr == "" || expr == "#object" && len(s.Properties) == 0 && s.Type == "" {
		return ""
	}
	return "* match response == " + giExpr(expr)
}

func objectShape(s schema, depth int) string {
	keys := matchKeys(s)
	if len(keys) == 0 || depth >= 2 {
		return "#object"
	}
	optional := len(s.Required) == 0
	required := map[string]bool{}
	for _, name := range s.Required {
		required[name] = true
	}
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, jsKey(key)+": "+giExpr(shape(s.Properties[key], optional || !required[key], depth+1)))
	}
	return "{ " + strings.Join(parts, ", ") + " }"
}

func matchKeys(s schema) []string {
	if len(s.Required) > 0 {
		keys := append([]string(nil), s.Required...)
		slices.Sort(keys)
		return keys
	}
	return slices.Sorted(maps.Keys(s.Properties))
}

func shape(s schema, optional bool, depth int) string {
	if !optional {
		if expr, ok := pinned(s); ok {
			return expr
		}
	}
	if s.Type == "array" || s.Items != nil {
		if optional {
			return "##array"
		}
		if s.Items != nil && depth < 2 {
			item := shape(*s.Items, false, depth+1)
			if strings.HasPrefix(item, "#") {
				return "#[] " + item
			}
		}
		return "#array"
	}
	if s.Type == "object" || len(s.Properties) > 0 {
		if optional || depth >= 2 || len(s.Properties) == 0 {
			return mark("#object", optional)
		}
		return objectShape(s, depth)
	}
	kind := "#string"
	switch s.Type {
	case "integer", "number":
		kind = "#number"
	case "boolean":
		kind = "#boolean"
	case "string":
		if s.Format == "uuid" {
			kind = "#uuid"
		}
	}
	return mark(kind, optional)
}

func pinned(s schema) (string, bool) {
	if s.Type == "object" || s.Type == "array" || s.Items != nil || len(s.Properties) > 0 {
		return "", false
	}
	if s.Example != nil {
		return literal(s.Example), true
	}
	if len(s.Enum) == 1 {
		return literal(s.Enum[0]), true
	}
	return "", false
}

func giExpr(expr string) string {
	if strings.HasPrefix(expr, "#") {
		return quote(expr)
	}
	return expr
}

func mark(kind string, optional bool) string {
	if optional && strings.HasPrefix(kind, "#") && !strings.HasPrefix(kind, "##") {
		return "#" + kind
	}
	return kind
}

func sample(name string, s schema) any {
	if s.Example != nil {
		return s.Example
	}
	if len(s.Enum) > 0 {
		return s.Enum[0]
	}
	if s.Type == "object" || len(s.Properties) > 0 {
		obj := map[string]any{}
		required := map[string]bool{}
		for _, key := range s.Required {
			required[key] = true
		}
		for _, key := range matchKeys(s) {
			if len(required) > 0 && !required[key] {
				continue
			}
			obj[key] = sample(key, s.Properties[key])
		}
		return obj
	}
	if s.Type == "array" {
		return []any{}
	}
	if s.Type == "string" || s.Type == "" {
		if name != "" {
			return name
		}
	}
	return placeholder(s.Type, name)
}

func literal(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case string:
		return quote(x)
	case bool:
		if x {
			return "true"
		}
		return "false"
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case uint64:
		return strconv.FormatUint(x, 10)
	case float64:
		if !math.IsInf(x, 0) && x == math.Trunc(x) {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	case float32:
		return literal(float64(x))
	case []any:
		parts := make([]string, len(x))
		for i, item := range x {
			parts[i] = literal(item)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case map[string]any:
		keys := slices.Sorted(maps.Keys(x))
		parts := make([]string, 0, len(keys))
		for _, key := range keys {
			parts = append(parts, jsKey(key)+": "+literal(x[key]))
		}
		if len(parts) == 0 {
			return "{}"
		}
		return "{ " + strings.Join(parts, ", ") + " }"
	default:
		return quote(fmt.Sprint(x))
	}
}

func quote(s string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(s) + "'"
}

func jsKey(s string) string {
	if ident(s) {
		return s
	}
	return quote(s)
}

func ident(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if i == 0 && !letter(r) && r != '_' {
			return false
		}
		if !letter(r) && !digit(r) && r != '_' {
			return false
		}
	}
	return true
}

func letter(r rune) bool { return r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' }
func digit(r rune) bool  { return r >= '0' && r <= '9' }

func slug(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			dash = false
			continue
		}
		if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

var (
	vusRE  = regexp.MustCompile(`(?i)(\d+)\s+(?:concurrent|virtual)\s+users|\b(\d+)\s+vus\b`)
	rateRE = regexp.MustCompile(`(?i)(\d+)\s+(?:rps|requests per second|req/s)`)
	p95RE  = regexp.MustCompile(`(?i)p95(?:[^0-9]{0,24})(\d+)\s*ms`)
)

func loadLines(criteria []string) []string {
	var vus, rate, p95 int
	for _, line := range criteria {
		if vus == 0 {
			vus = firstInt(vusRE, line)
		}
		if rate == 0 {
			rate = firstInt(rateRE, line)
		}
		if p95 == 0 {
			p95 = firstInt(p95RE, line)
		}
	}
	var lines []string
	if vus > 0 {
		lines = append(lines, fmt.Sprintf("# gi-load: name=spec vus=%d duration=30s", vus))
		if p95 > 0 {
			lines = append(lines, fmt.Sprintf("# gi-load: threshold p95 < %dms", p95))
		}
	}
	if rate > 0 {
		name := "spec"
		if vus > 0 {
			name = "rate"
		}
		lines = append(lines, fmt.Sprintf("# gi-load: name=%s rate=%d duration=30s", name, rate))
		if p95 > 0 {
			lines = append(lines, fmt.Sprintf("# gi-load: threshold p95 < %dms", p95))
		}
	}
	return lines
}

func firstInt(re *regexp.Regexp, s string) int {
	m := re.FindStringSubmatch(s)
	if m == nil {
		return 0
	}
	for _, group := range m[1:] {
		if group == "" {
			continue
		}
		n, err := strconv.Atoi(group)
		if err == nil {
			return n
		}
	}
	return 0
}
