package engine

import (
	"github.com/gi-runner/gi/internal/match"
	"testing"
)

func TestSplitMatchNestedExpressions(t *testing.T) {
	for _, lhs := range []string{
		`rows.filter(x => x.id == 1)`,
		`(a != b)`,
		`({a: 'contains', b: [1, 2]})`,
		"`value == ${1}`",
		`rows.filter(x => /a==b/.test(x))`,
		`/a==b/.test(text)`,
		`value /* == ignored */`,
		`response/root/item[@id='a==b']`,
		`//item`,
		`response//item`,
		`/root/*`,
	} {
		got, mode, rhs, ok := splitMatch(lhs + " == true")
		if !ok || got != lhs || mode != match.Equals || rhs != "true" {
			t.Fatalf("%s: %q %v %q %v", lhs, got, mode, rhs, ok)
		}
	}
}
