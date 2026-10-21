package match_test

import (
	"testing"

	"github.com/gi-runner/gi/internal/match"
	"github.com/gi-runner/gi/internal/script"
	"github.com/gi-runner/gi/internal/value"
)

func ctx(vars map[string]any) match.Context {
	vm := script.New()
	for k, v := range vars {
		vm.Set(k, v)
	}
	return match.Context{Truthy: vm.Truthy, Eval: vm.Eval}
}

func mustPass(t *testing.T, res match.Result) {
	t.Helper()
	if !res.Pass {
		t.Fatalf("expected pass\n%s", res.Message)
	}
}

func mustFail(t *testing.T, res match.Result) {
	t.Helper()
	if res.Pass {
		t.Fatal("expected failure")
	}
	if res.Message == "" {
		t.Fatal("expected a diff")
	}
}

func TestSchemaFromTheBrief(t *testing.T) {
	actual := map[string]any{
		"id":   "550e8400-e29b-41d4-a716-446655440000",
		"name": "Ada",
		"age":  int64(36),
		"tags": []any{"math", "poet"},
	}
	expected := map[string]any{
		"id":    "#uuid",
		"name":  "#string",
		"age":   "#number",
		"email": "##string",
		"tags":  "#[] #string",
	}
	mustPass(t, match.Match(actual, expected, match.Equals, ctx(nil)))
}

func TestOptionalNullVersusMissing(t *testing.T) {
	withNull := map[string]any{"name": "Ada", "email": nil}
	missing := map[string]any{"name": "Ada"}
	schema := map[string]any{"name": "#string", "email": "##string"}
	mustPass(t, match.Match(withNull, schema, match.Equals, ctx(nil)))
	mustPass(t, match.Match(missing, schema, match.Equals, ctx(nil)))
	mustFail(t, match.Match(map[string]any{"name": "Ada", "email": int64(1)}, schema, match.Equals, ctx(nil)))
	mustFail(t, match.Match(map[string]any{"name": "Ada"}, map[string]any{"name": "#string", "email": "#string"}, match.Equals, ctx(nil)))
	mustPass(t, match.Match(map[string]any{"a": nil}, map[string]any{"a": "#null"}, match.Equals, ctx(nil)))
	mustFail(t, match.Match(map[string]any{}, map[string]any{"a": "#null"}, match.Equals, ctx(nil)))
	mustPass(t, match.Match(map[string]any{}, map[string]any{"a": "#notpresent"}, match.Equals, ctx(nil)))
	mustPass(t, match.Match(value.Absent{}, "#notpresent", match.Equals, ctx(nil)))
	mustFail(t, match.Match(map[string]any{"a": nil}, map[string]any{"a": "#notpresent"}, match.Equals, ctx(nil)))
	mustPass(t, match.Match(map[string]any{"a": nil}, map[string]any{"a": "#present"}, match.Equals, ctx(nil)))
}

func TestContainsDeepAndEach(t *testing.T) {
	actual := map[string]any{
		"id": int64(1),
		"address": map[string]any{
			"city": "Paris",
			"zip":  "75001",
		},
	}
	partial := map[string]any{"id": int64(1), "address": map[string]any{"city": "Paris"}}
	mustPass(t, match.Match(actual, partial, match.ContainsDeep, ctx(nil)))
	mustFail(t, match.Match(actual, partial, match.Contains, ctx(nil)))
	mustPass(t, match.Match(actual, map[string]any{"id": int64(1)}, match.Contains, ctx(nil)))

	items := make([]any, 0, 3)
	items = append(items,
		map[string]any{"active": true, "id": int64(1)},
		map[string]any{"active": true, "id": int64(2)},
	)
	mustPass(t, match.MatchEach(items, map[string]any{"active": true}, match.Contains, ctx(nil)))
	mustFail(t, match.MatchEach(append(items, map[string]any{"active": false}), map[string]any{"active": true}, match.Contains, ctx(nil)))
	mustFail(t, match.MatchEach([]any{}, map[string]any{"active": true}, match.Contains, ctx(nil)))
}

func TestContainsOnlyAndWildcardShape(t *testing.T) {
	ids := []any{"b", "a"}
	mustPass(t, match.Match(ids, []any{"a", "b"}, match.ContainsOnly, ctx(nil)))
	mustFail(t, match.Match(ids, []any{"a"}, match.ContainsOnly, ctx(nil)))
	mustPass(t, match.Match([]any{"a", "b"}, []any{"#string", "a"}, match.ContainsOnly, ctx(nil)))
	mustPass(t, match.Match([]any{"admin", "verified"}, "admin", match.Contains, ctx(nil)))
}

func TestPredicate(t *testing.T) {
	c := ctx(map[string]any{"min": int64(1), "max": int64(100)})
	mustPass(t, match.Match(int64(150), "#? _ > 100", match.Equals, c))
	mustFail(t, match.Match(int64(50), "#? _ > 100", match.Equals, c))
	mustPass(t, match.Match(int64(50), "#? _ >= min && _ <= max", match.Equals, c))
	price := map[string]any{"celsius": int64(100), "fahrenheit": int64(212)}
	mustPass(t, match.Match(price, map[string]any{
		"celsius":    "#number",
		"fahrenheit": "#? _ == $.celsius * 1.8 + 32",
	}, match.Equals, c))
}

func TestArraySchemaAndRegex(t *testing.T) {
	mustPass(t, match.Match([]any{"bar", "baz"}, "#[2] #string", match.Equals, ctx(nil)))
	mustFail(t, match.Match([]any{"bar", "baz"}, "#[3]", match.Equals, ctx(nil)))
	mustPass(t, match.Match([]any{"bar", "baz"}, "#[]? _.length == 3", match.Equals, ctx(nil)))
	mustPass(t, match.Match("Ada", "#regex Ada.*", match.Equals, ctx(nil)))
	mustFail(t, match.Match("Grace", "#regex Ada.*", match.Equals, ctx(nil)))
}

func TestNotEqualsAndWithin(t *testing.T) {
	mustPass(t, match.Match("ok", "error", match.NotEquals, ctx(nil)))
	mustFail(t, match.Match("ok", "ok", match.NotEquals, ctx(nil)))
	mustPass(t, match.Match(int64(36), map[string]any{"low": int64(30), "high": int64(40)}, match.Within, ctx(nil)))
	mustFail(t, match.Match(int64(20), map[string]any{"low": int64(30), "high": int64(40)}, match.Within, ctx(nil)))
}

func BenchmarkContainsOnlyOverlapping(b *testing.B) {
	actual := make([]any, 64)
	expected := make([]any, 64)
	for i := range actual {
		actual[i] = int64(i)
		expected[i] = "#number"
	}
	for b.Loop() {
		if !match.Match(actual, expected, match.ContainsOnly, match.Context{}).Pass {
			b.Fatal("match failed")
		}
	}
}

func TestBytesEquality(t *testing.T) {
	for _, tc := range []struct {
		a, b any
		mode match.Mode
		pass bool
	}{
		{[]byte("abc"), []byte("abc"), match.Equals, true},
		{[]byte("abc"), []byte("abd"), match.Equals, false},
		{[]byte("abc"), []byte("abd"), match.NotEquals, true},
		{map[string]any{"data": []byte("abc")}, map[string]any{"data": []byte("abc")}, match.Equals, true},
	} {
		if got := match.Match(tc.a, tc.b, tc.mode, match.Context{}); got.Pass != tc.pass {
			t.Fatalf("%v: %s", tc, got.Message)
		}
	}
}

func TestValidatorErrorsAreDistinctFromMismatches(t *testing.T) {
	for _, mode := range []match.Mode{match.Equals, match.NotEquals} {
		result := match.Match(1, "#? missingFunction()", mode, ctx(nil))
		if result.Pass || result.Err == nil {
			t.Fatalf("mode %v: %+v", mode, result)
		}
	}
	result := match.MatchEach([]any{1}, "#? missingFunction()", match.Equals, ctx(nil))
	if result.Err == nil {
		t.Fatalf("each dropped error: %+v", result)
	}
}

func TestMatchEachKeepsPredicateRootAndNegation(t *testing.T) {
	values := []any{int64(1), int64(2), int64(3)}
	mustPass(t, match.MatchEach(values, "#? _ <= $.length", match.Equals, ctx(nil)))
	mustPass(t, match.MatchEach(values, "#? _ <= $.limit", match.Equals, func() match.Context { c := ctx(nil); c.Root = map[string]any{"limit": 3}; return c }()))
	mustPass(t, match.MatchEach(values, int64(4), match.NotEquals, ctx(nil)))
	mustFail(t, match.MatchEach(values, int64(2), match.NotEquals, ctx(nil)))
}

func TestUnorderedLiteralCountsAndPrecision(t *testing.T) {
	mustPass(t, match.Match([]any{int64(1), int64(1), 2.0}, []any{int64(2), 1.0, int64(1)}, match.ContainsOnly, ctx(nil)))
	mustFail(t, match.Match([]any{int64(1), int64(2), int64(2)}, []any{int64(1), int64(1), int64(2)}, match.ContainsOnly, ctx(nil)))
	mustFail(t, match.Match([]any{int64(9007199254740993)}, []any{float64(9007199254740992)}, match.ContainsOnly, ctx(nil)))
	mustPass(t, match.Match([]any{nil, true, "plain", 1.25}, []any{1.25, "plain", true, nil}, match.ContainsOnly, ctx(nil)))
}

type arrayModel struct{ items []any }

func (m arrayModel) DataValue() any { return m.items }

func TestUnorderedDataModelNormalizesNumericKeys(t *testing.T) {
	mustPass(t, match.Match(arrayModel{items: []any{float64(2)}}, []any{int64(2)}, match.ContainsOnly, ctx(nil)))
}
