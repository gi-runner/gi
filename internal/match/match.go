// A missing key is not null. ##string allows missing, null, or a string.
package match

import (
	"bytes"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"uuid"

	"github.com/gi-runner/gi/internal/value"
)

type Mode int

const (
	Equals Mode = iota
	NotEquals
	Contains
	NotContains
	ContainsOnly
	ContainsDeep
	ContainsOnlyDeep
	ContainsAny
	ContainsAnyDeep
	Within
	NotWithin
)

func (m Mode) String() string {
	switch m {
	case Equals:
		return "=="
	case NotEquals:
		return "!="
	case Contains:
		return "contains"
	case NotContains:
		return "!contains"
	case ContainsOnly:
		return "contains only"
	case ContainsDeep:
		return "contains deep"
	case ContainsOnlyDeep:
		return "contains only deep"
	case ContainsAny:
		return "contains any"
	case ContainsAnyDeep:
		return "contains any deep"
	case Within:
		return "within"
	case NotWithin:
		return "!within"
	default:
		return "match"
	}
}

type Context struct {
	err    error
	Root   any
	Truthy func(expr string, self, root any) (bool, error)
	Eval   func(expr string) (any, error)
}

type Failure struct {
	Path     string
	Reason   string
	Actual   string
	Expected string
}

type Result struct {
	Err      error
	Pass     bool
	Message  string
	Failures []Failure
}

func Match(actual, expected any, mode Mode, ctx Context) Result {
	actual = value.Normalize(actual)
	expected = value.Normalize(expected)
	if ctx.Root == nil {
		ctx.Root = actual
	} else {
		ctx.Root = value.Normalize(ctx.Root)
	}
	return matchNormalized(actual, expected, mode, ctx)
}

// Inputs and the predicate root are detached once at the public boundary.
func matchNormalized(actual, expected any, mode Mode, ctx Context) Result {
	neg := mode == NotEquals || mode == NotContains || mode == NotWithin
	inner := mode
	switch mode {
	case NotEquals:
		inner = Equals
	case NotContains:
		inner = Contains
	case NotWithin:
		inner = Within
	case Equals, Contains, ContainsOnly, ContainsDeep, ContainsOnlyDeep, ContainsAny, ContainsAnyDeep, Within:
	}
	fails := matchValue(actual, expected, inner, "$", &ctx)
	if ctx.err != nil {
		failures := fail("$", "validator evaluation failed: "+ctx.err.Error(), actual, expected)
		return Result{Err: ctx.err, Message: formatMessage(mode, failures), Failures: failures}
	}
	if neg {
		if len(fails) == 0 {
			reason := "values are equal"
			if mode == NotContains {
				reason = "actual contains expected"
			}
			if mode == NotWithin {
				reason = "actual is within expected"
			}
			f := Failure{Path: "$", Reason: reason, Actual: value.Format(actual), Expected: value.Format(expected)}
			return Result{Message: formatMessage(mode, []Failure{f}), Failures: []Failure{f}}
		}
		return Result{Pass: true}
	}
	if len(fails) == 0 {
		return Result{Pass: true}
	}
	return Result{Message: formatMessage(mode, fails), Failures: fails}
}

func MatchEach(actual, expected any, mode Mode, ctx Context) Result {
	actual = value.Normalize(actual)
	expected = value.Normalize(expected)
	arr, ok := value.AsSlice(actual)
	if !ok {
		f := Failure{Path: "$", Reason: "actual is not an array", Actual: value.Format(actual), Expected: value.Format(expected)}
		return Result{Message: formatMessage(mode, []Failure{f}), Failures: []Failure{f}}
	}
	if len(arr) == 0 {
		f := Failure{Path: "$", Reason: "match each failed, empty array", Actual: "[]", Expected: value.Format(expected)}
		return Result{Message: formatMessage(mode, []Failure{f}), Failures: []Failure{f}}
	}
	if ctx.Root == nil {
		ctx.Root = actual
	} else {
		ctx.Root = value.Normalize(ctx.Root)
	}
	var fails []Failure
	var idxs []int
	for i, el := range arr {
		child := matchNormalized(el, expected, mode, ctx)
		if child.Err != nil {
			return child
		}
		if child.Pass {
			continue
		}
		idxs = append(idxs, i)
		fails = append(fails, child.Failures...)
		if len(idxs) >= 5 {
			break
		}
	}
	if len(idxs) == 0 {
		return Result{Pass: true}
	}
	reason := fmt.Sprintf("match each failed at index %d", idxs[0])
	if len(idxs) > 1 {
		reason = fmt.Sprintf("match each failed at indices %v", idxs)
	}
	head := Failure{Path: "$", Reason: reason, Actual: value.Format(actual), Expected: value.Format(expected)}
	all := append([]Failure{head}, fails...)
	return Result{Message: formatMessage(mode, all), Failures: all}
}

func formatMessage(mode Mode, fails []Failure) string {
	var b strings.Builder
	fmt.Fprintf(&b, "match failed: %s\n", strings.ToUpper(strings.ReplaceAll(mode.String(), " ", "_")))
	limit := fails
	if len(limit) > 20 {
		limit = limit[:20]
	}
	for i, f := range limit {
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "  %s | %s\n", f.Path, f.Reason)
		fmt.Fprintf(&b, "    actual:   %s\n", indent(f.Actual, "             "))
		fmt.Fprintf(&b, "    expected: %s", indent(f.Expected, "             "))
	}
	if len(fails) > len(limit) {
		fmt.Fprintf(&b, "\n  … %d more", len(fails)-len(limit))
	}
	return b.String()
}

func indent(s, pad string) string {
	if !strings.Contains(s, "\n") {
		return s
	}
	return strings.ReplaceAll(s, "\n", "\n"+pad)
}

func matchValue(actual, expected any, mode Mode, path string, ctx *Context) []Failure {
	if a, ok := actual.(interface{ Comparable() any }); ok {
		if b, ok := expected.(interface{ Comparable() any }); ok {
			actual, expected = a.Comparable(), b.Comparable()
		}
	}
	if model, ok := actual.(interface{ DataValue() any }); ok {
		actual = model.DataValue()
	}
	if model, ok := expected.(interface{ DataValue() any }); ok {
		expected = model.DataValue()
	}
	if mode == Within {
		return matchWithin(actual, expected, path)
	}
	if isContainsMode(mode) && !sameShape(actual, expected) && !value.IsAbsent(actual) {
		if _, isSlice := value.AsSlice(expected); !isSlice && !isMacroRef(expected) {
			return matchValue(actual, []any{expected}, mode, path, ctx)
		}
	}
	if s, ok := expected.(string); ok && strings.HasPrefix(s, "#") {
		if m, ok := parseMarker(s); ok {
			return matchMarker(actual, m, s, mode, path, ctx)
		}
	}
	if value.IsAbsent(actual) {
		return fail(path, "actual path does not exist", actual, expected)
	}
	if !sameShape(actual, expected) {
		return fail(path, fmt.Sprintf("data types don't match (%s vs %s)", value.Kind(actual), value.Kind(expected)), actual, expected)
	}
	switch mode {
	case Contains, ContainsAny, ContainsOnly, ContainsDeep, ContainsOnlyDeep, ContainsAnyDeep:
		return matchContains(actual, expected, mode, path, ctx)
	case Equals, NotEquals, NotContains, Within, NotWithin:
		return matchEquals(actual, expected, path, ctx)
	default:
		return matchEquals(actual, expected, path, ctx)
	}
}

func matchEquals(actual, expected any, path string, ctx *Context) []Failure {
	if equal, handled := scalarEqual(actual, expected); handled {
		if equal {
			return nil
		}
		return fail(path, "not equal", actual, expected)
	}
	switch a := actual.(type) {
	case []any:
		exp, ok := expected.([]any)
		if !ok {
			return fail(path, "not equal", actual, expected)
		}
		if len(a) != len(exp) {
			return fail(path, fmt.Sprintf("array length %d != %d", len(a), len(exp)), actual, expected)
		}
		var fails []Failure
		for i := range a {
			fails = append(fails, matchValue(a[i], exp[i], Equals, fmt.Sprintf("%s[%d]", path, i), ctx)...)
		}
		return fails
	case map[string]any:
		exp, ok := expected.(map[string]any)
		if !ok {
			return fail(path, "not equal", actual, expected)
		}
		return matchMaps(a, exp, Equals, path, ctx)
	default:
		if reflect.ValueOf(actual).Comparable() && reflect.ValueOf(expected).Comparable() && actual == expected {
			return nil
		}
		return fail(path, "not equal", actual, expected)
	}
}

// Literal candidates can be compared without constructing discarded diagnostics.
func scalarEqual(actual, expected any) (equal, handled bool) {
	switch a := actual.(type) {
	case nil:
		return expected == nil, true
	case bool:
		b, ok := expected.(bool)
		return ok && a == b, true
	case int64, float64:
		equal, _ := value.NumbersEqual(actual, expected)
		return equal, true
	case string:
		b, ok := expected.(string)
		return ok && a == b, true
	case []byte:
		b, ok := expected.([]byte)
		return ok && bytes.Equal(a, b), true
	default:
		return false, false
	}
}

func pairMatches(actual, expected any, mode Mode, ctx *Context) bool {
	marker, ok := expected.(string)
	if !ok || !strings.HasPrefix(marker, "#") {
		if equal, handled := scalarEqual(actual, expected); handled {
			return equal
		}
	}
	return len(matchValue(actual, expected, childMode(mode, actual), "$", ctx)) == 0
}

func matchContains(actual, expected any, mode Mode, path string, ctx *Context) []Failure {
	switch a := actual.(type) {
	case string:
		e, ok := expected.(string)
		if !ok {
			return fail(path, "contains requires a string on the right", actual, expected)
		}
		if strings.Contains(a, e) {
			return nil
		}
		return fail(path, "string does not contain expected", actual, expected)
	case []any:
		exp, ok := value.AsSlice(expected)
		if !ok {
			return fail(path, "contains requires an array on the right", actual, expected)
		}
		return matchLists(a, exp, mode, path, ctx)
	case map[string]any:
		exp, ok := value.AsMap(expected)
		if !ok {
			return fail(path, "contains requires an object on the right", actual, expected)
		}
		return matchMaps(a, exp, mode, path, ctx)
	default:
		return fail(path, "contains is not defined for "+value.Kind(actual), actual, expected)
	}
}

func matchLists(actual, expected []any, mode Mode, path string, ctx *Context) []Failure {
	if mode != ContainsAny && mode != ContainsAnyDeep && len(expected) > len(actual) {
		return fail(path, fmt.Sprintf("array length %d < %d", len(actual), len(expected)), actual, expected)
	}
	if (mode == ContainsOnly || mode == ContainsOnlyDeep) && len(actual) != len(expected) {
		return fail(path, fmt.Sprintf("array length %d != %d", len(actual), len(expected)), actual, expected)
	}
	if mode == ContainsOnly || mode == ContainsOnlyDeep {
		if !assignAll(actual, expected, mode, ctx) {
			return fail(path, "array does not contain only the expected items", actual, expected)
		}
		return nil
	}
	var missing []string
	for _, exp := range expected {
		found := false
		for _, act := range actual {
			child := childMode(mode, act)
			if len(matchValue(act, exp, child, path, ctx)) == 0 {
				found = true
				if mode == ContainsAny || mode == ContainsAnyDeep {
					return nil
				}
				break
			}
		}
		if !found && mode != ContainsAny && mode != ContainsAnyDeep {
			missing = append(missing, value.Format(exp))
		}
	}
	if mode == ContainsAny || mode == ContainsAnyDeep {
		return fail(path, "array does not contain any expected item", actual, expected)
	}
	if len(missing) > 0 {
		return fail(path, "array does not contain expected item", actual, expected)
	}
	return nil
}

// Each expected item is paired with one actual item. Pairs are scored once.
func assignAll(actual, expected []any, mode Mode, ctx *Context) bool {
	if equal, handled := literalBagEqual(actual, expected); handled {
		return equal
	}
	edges := make([][]int, len(expected))
	for i, exp := range expected {
		for j, act := range actual {
			if pairMatches(act, exp, mode, ctx) {
				edges[i] = append(edges[i], j)
			}
		}
	}
	return perfectMatching(edges, len(actual))
}

func matchMaps(actual, expected map[string]any, mode Mode, path string, ctx *Context) []Failure {
	var extra []string
	for k := range actual {
		if _, ok := expected[k]; !ok {
			extra = append(extra, k)
		}
	}
	sort.Strings(extra)
	var fails []Failure
	if len(extra) > 0 && (mode == Equals || mode == ContainsOnly || mode == ContainsOnlyDeep) {
		fails = append(fails, fail(path, "unexpected keys: "+strings.Join(extra, ", "), actual, expected)...)
	}
	var missing []string
	var childFails []Failure
	matched := 0
	keys := make([]string, 0, len(expected))
	for k := range expected {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		exp := expected[k]
		act, ok := actual[k]
		childPath := value.JoinPath(path, k)
		if !ok {
			if allowsMissing(exp) {
				if mode == ContainsAny || mode == ContainsAnyDeep {
					return nil
				}
				continue
			}
			if mode == ContainsAny || mode == ContainsAnyDeep {
				continue
			}
			missing = append(missing, k)
			continue
		}
		cf := matchValue(act, exp, childMode(mode, act), childPath, ctx)
		if len(cf) == 0 {
			matched++
			if mode == ContainsAny || mode == ContainsAnyDeep {
				return nil
			}
			continue
		}
		childFails = append(childFails, cf...)
	}
	if len(missing) > 0 {
		fails = append(fails, fail(path, "missing keys: "+strings.Join(missing, ", "), actual, expected)...)
	}
	if len(childFails) > 0 && mode != ContainsAny && mode != ContainsAnyDeep {
		fails = append(fails, childFails...)
	}
	if (mode == ContainsAny || mode == ContainsAnyDeep) && matched == 0 && len(expected) > 0 {
		return fail(path, "no key matched", actual, expected)
	}
	return fails
}

func childMode(mode Mode, actual any) Mode {
	composite := false
	switch actual.(type) {
	case []any, map[string]any:
		composite = true
	}
	switch mode {
	case ContainsDeep:
		if composite {
			return ContainsDeep
		}
	case ContainsOnlyDeep:
		if composite {
			return ContainsOnlyDeep
		}
	case ContainsAnyDeep:
		if composite {
			return ContainsAny
		}
	case Equals, NotEquals, Contains, NotContains, ContainsOnly, ContainsAny, Within, NotWithin:
	}
	return Equals
}

func matchWithin(actual, expected any, path string) []Failure {
	if m, ok := value.AsMap(expected); ok {
		low, lok := m["low"]
		high, hok := m["high"]
		if lok && hok && value.IsNumber(actual) {
			cl, ok1 := value.CompareNumbers(actual, low)
			ch, ok2 := value.CompareNumbers(actual, high)
			if ok1 && ok2 && cl >= 0 && ch <= 0 {
				return nil
			}
			return fail(path, "not within low..high", actual, expected)
		}
	}
	switch a := actual.(type) {
	case string:
		e, ok := expected.(string)
		if ok && strings.Contains(e, a) {
			return nil
		}
		return fail(path, "expected string does not contain actual", actual, expected)
	case []any:
		exp, ok := value.AsSlice(expected)
		if !ok {
			return fail(path, "within requires an array on the right", actual, expected)
		}
		if len(a) > len(exp) {
			return fail(path, "actual array is longer than expected", actual, expected)
		}
		for i, item := range a {
			found := false
			for _, want := range exp {
				if bothEqual(item, want) {
					found = true
					break
				}
			}
			if !found {
				return fail(path, fmt.Sprintf("expected does not contain actual item at [%d]", i), actual, expected)
			}
		}
		return nil
	default:
		return fail(path, "within is not defined for these values", actual, expected)
	}
}

func bothEqual(a, b any) bool {
	if !sameShape(a, b) {
		return false
	}
	return len(matchEquals(a, b, "$", &Context{})) == 0
}

type marker struct {
	optional bool
	kind     string
	pred     string
	regex    string
	arrayLen string
	elem     string
	embed    string
}

func parseMarker(s string) (marker, bool) {
	if !strings.HasPrefix(s, "#") {
		return marker{}, false
	}
	rest := s[1:]
	optional := false
	if strings.HasPrefix(rest, "#") {
		optional = true
		rest = rest[1:]
	}
	if rest == "" {
		return marker{}, false
	}
	if strings.HasPrefix(rest, "(") && strings.HasSuffix(rest, ")") && parenWraps(rest) {
		return marker{optional: optional, kind: "embed", embed: rest[1 : len(rest)-1]}, true
	}
	if strings.HasPrefix(rest, "[") {
		end := strings.Index(rest, "]")
		if end < 0 {
			return marker{}, false
		}
		return marker{
			optional: optional,
			kind:     "array",
			arrayLen: rest[1:end],
			elem:     strings.TrimSpace(rest[end+1:]),
		}, true
	}
	if strings.HasPrefix(rest, "?") {
		return marker{optional: optional, kind: "predicate", pred: strings.TrimSpace(rest[1:])}, true
	}
	if rest == "regex" || strings.HasPrefix(rest, "regex ") {
		return marker{optional: optional, kind: "regex", regex: strings.TrimSpace(strings.TrimPrefix(rest, "regex"))}, true
	}
	name := rest
	pred := ""
	if i := strings.IndexByte(rest, '?'); i >= 0 {
		name = strings.TrimSpace(rest[:i])
		pred = strings.TrimSpace(rest[i+1:])
	}
	switch name {
	case "string", "number", "boolean", "array", "object", "null", "notnull", "present", "ignore", "uuid", "notpresent":
		return marker{optional: optional, kind: name, pred: pred}, true
	default:
		return marker{}, false
	}
}

func parenWraps(s string) bool {
	depth := 0
	for i := range len(s) {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i == len(s)-1
			}
		}
	}
	return false
}

var (
	regexMu    sync.RWMutex
	regexCache = make(map[string]*regexp.Regexp)
)

func getRegex(pattern string) (*regexp.Regexp, error) {
	regexMu.RLock()
	re := regexCache[pattern]
	regexMu.RUnlock()
	if re != nil {
		return re, nil
	}
	re, err := regexp.Compile(`\A(?:` + pattern + `)\z`)
	if err != nil {
		return nil, err
	}
	regexMu.Lock()
	// Large expressions compile normally without occupying the process-wide cache.
	if len(regexCache) < 512 && len(pattern) <= 4096 {
		regexCache[pattern] = re
	}
	regexMu.Unlock()
	return re, nil
}

func matchMarker(actual any, m marker, raw string, mode Mode, path string, ctx *Context) []Failure {
	if m.kind == "embed" {
		if ctx.Eval == nil {
			return fail(path, "embedded expression requires a script runtime", actual, raw)
		}
		got, err := ctx.Eval(m.embed)
		if err != nil {
			ctx.err = err
			return fail(path, "embedded expression failed: "+err.Error(), actual, raw)
		}
		return matchValue(actual, value.Normalize(got), mode, path, ctx)
	}
	if value.IsAbsent(actual) {
		switch m.kind {
		case "notpresent", "ignore":
			return nil
		}
		if m.optional {
			return nil
		}
		return fail(path, "actual path does not exist", actual, raw)
	}
	if m.optional && actual == nil && m.kind != "notpresent" && m.kind != "present" && m.kind != "notnull" {
		return nil
	}
	if m.kind == "array" {
		return matchArrayMarker(actual, m, raw, path, ctx)
	}
	if m.kind == "regex" {
		if _, err := getRegex(m.regex); err != nil {
			ctx.err = err
			return fail(path, "invalid regex", actual, raw)
		}
	}
	if !validatorOK(actual, m) {
		return fail(path, validatorReason(m), actual, raw)
	}
	if m.pred != "" {
		return runPred(actual, m.pred, raw, path, ctx)
	}
	return nil
}

func validatorOK(actual any, m marker) bool {
	switch m.kind {
	case "ignore":
		return true
	case "null":
		return actual == nil
	case "notnull":
		return actual != nil
	case "present":
		return !value.IsAbsent(actual)
	case "notpresent":
		return value.IsAbsent(actual)
	case "string":
		_, ok := actual.(string)
		return ok
	case "number":
		return value.IsNumber(actual)
	case "boolean":
		_, ok := actual.(bool)
		return ok
	case "array":
		_, ok := value.AsSlice(actual)
		return ok
	case "object":
		_, ok := value.AsMap(actual)
		return ok
	case "uuid":
		s, ok := actual.(string)
		if !ok {
			return false
		}
		id, err := uuid.Parse(s)
		return err == nil && id.String() == strings.ToLower(s)
	case "regex":
		s, ok := actual.(string)
		if !ok {
			return false
		}
		re, err := getRegex(m.regex)
		if err != nil {
			return false
		}
		return re.MatchString(s)
	case "predicate":
		return true
	default:
		return false
	}
}

func validatorReason(m marker) string {
	switch m.kind {
	case "null":
		return "not null"
	case "notnull":
		return "null"
	case "present":
		return "not present"
	case "notpresent":
		return "present"
	case "string":
		return "not a string"
	case "number":
		return "not a number"
	case "boolean":
		return "not a boolean"
	case "array":
		return "not an array"
	case "object":
		return "not an object"
	case "uuid":
		return "not a uuid"
	case "regex":
		return "regex did not match"
	default:
		return "validator " + m.kind + " failed"
	}
}

func runPred(actual any, pred, raw, path string, ctx *Context) []Failure {
	if ctx.Truthy == nil {
		return fail(path, "predicate requires a script runtime", actual, raw)
	}
	ok, err := ctx.Truthy(pred, actual, ctx.Root)
	if err != nil {
		ctx.err = err
		return fail(path, "predicate failed: "+err.Error(), actual, raw)
	}
	if !ok {
		return fail(path, "predicate evaluated to false", actual, raw)
	}
	return nil
}

func matchArrayMarker(actual any, m marker, raw, path string, ctx *Context) []Failure {
	if m.optional && (actual == nil || value.IsAbsent(actual)) {
		return nil
	}
	arr, ok := value.AsSlice(actual)
	if !ok {
		return fail(path, "not an array", actual, raw)
	}
	if m.arrayLen != "" {
		if f := checkLen(arr, m.arrayLen, raw, path, ctx); f != nil {
			return f
		}
	}
	elem := strings.TrimSpace(m.elem)
	if elem == "" {
		return nil
	}
	if strings.HasPrefix(elem, "?") {
		elem = "#" + elem
	}
	if strings.HasPrefix(elem, "#") {
		var fails []Failure
		for i, item := range arr {
			child := matchValue(item, elem, Equals, fmt.Sprintf("%s[%d]", path, i), ctx)
			fails = append(fails, child...)
			if len(fails) > 20 {
				break
			}
		}
		return fails
	}
	if ctx.Eval == nil {
		return fail(path, "array schema expression requires a script runtime", actual, raw)
	}
	schema, err := ctx.Eval(elem)
	if err != nil {
		ctx.err = err
		return fail(path, "array schema failed: "+err.Error(), actual, raw)
	}
	schema = value.Normalize(schema)
	mode := Equals
	expr := elem
	switch {
	case strings.HasPrefix(expr, "^^"):
		mode = ContainsOnly
		expr = strings.TrimSpace(expr[2:])
	case strings.HasPrefix(expr, "^+"):
		mode = ContainsDeep
		expr = strings.TrimSpace(expr[2:])
	case strings.HasPrefix(expr, "^*"):
		mode = ContainsAny
		expr = strings.TrimSpace(expr[2:])
	case strings.HasPrefix(expr, "^"):
		mode = Contains
		expr = strings.TrimSpace(expr[1:])
	}
	if expr != elem {
		schema, err = ctx.Eval(expr)
		if err != nil {
			ctx.err = err
			return fail(path, "array schema failed: "+err.Error(), actual, raw)
		}
		schema = value.Normalize(schema)
	}
	var fails []Failure
	for i, item := range arr {
		fails = append(fails, matchValue(item, schema, mode, fmt.Sprintf("%s[%d]", path, i), ctx)...)
	}
	return fails
}

func checkLen(arr []any, spec, raw, path string, ctx *Context) []Failure {
	n := len(arr)
	if i, err := strconv.Atoi(spec); err == nil {
		if n != i {
			return fail(path, fmt.Sprintf("array length is %d", n), n, raw)
		}
		return nil
	}
	expr := spec
	if !containsUnderscore(spec) {
		expr = spec + " == _"
	}
	if ctx.Truthy == nil {
		return fail(path, "array length expression requires a script runtime", n, raw)
	}
	ok, err := ctx.Truthy(expr, int64(n), ctx.Root)
	if err != nil {
		ctx.err = err
		return fail(path, "array length expression failed: "+err.Error(), n, raw)
	}
	if !ok {
		return fail(path, fmt.Sprintf("array length is %d", n), n, raw)
	}
	return nil
}

func containsUnderscore(s string) bool {
	for i := range len(s) {
		if s[i] != '_' {
			continue
		}
		before := i == 0 || !isIdentByte(s[i-1])
		after := i+1 == len(s) || !isIdentByte(s[i+1])
		if before && after {
			return true
		}
	}
	return false
}

func isIdentByte(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

func allowsMissing(exp any) bool {
	s, ok := exp.(string)
	if !ok {
		return false
	}
	return strings.HasPrefix(s, "##") || s == "#ignore" || s == "#notpresent"
}

func isMacroRef(exp any) bool {
	s, ok := exp.(string)
	if !ok {
		return false
	}
	return strings.HasPrefix(s, "#[") || strings.HasPrefix(s, "##[") ||
		strings.HasPrefix(s, "#(") || strings.HasPrefix(s, "##(") ||
		s == "#array" || s == "##array" || s == "#object" || s == "##object"
}

func isContainsMode(mode Mode) bool {
	switch mode {
	case Contains, NotContains, ContainsOnly, ContainsAny, ContainsDeep, ContainsOnlyDeep, ContainsAnyDeep:
		return true
	case Equals, NotEquals, Within, NotWithin:
		return false
	default:
		return false
	}
}

func sameShape(a, b any) bool {
	if value.IsAbsent(a) || value.IsAbsent(b) {
		return value.IsAbsent(a) && value.IsAbsent(b)
	}
	switch a.(type) {
	case nil:
		return b == nil
	case bool:
		_, ok := b.(bool)
		return ok
	case string:
		_, ok := b.(string)
		return ok
	case int64, float64:
		return value.IsNumber(b)
	case []any:
		_, ok := b.([]any)
		return ok
	case map[string]any:
		_, ok := b.(map[string]any)
		return ok
	default:
		return fmt.Sprintf("%T", a) == fmt.Sprintf("%T", b)
	}
}

func fail(path, reason string, actual, expected any) []Failure {
	return []Failure{{
		Path:     path,
		Reason:   reason,
		Actual:   value.Format(actual),
		Expected: value.Format(expected),
	}}
}
