package engine

import (
	"strconv"
	"testing"

	"github.com/gi-runner/gi/internal/feature"
	"github.com/gi-runner/gi/internal/match"
)

func TestSplitMatchNestedExpressions(t *testing.T) {
	for _, lhs := range []string{
		`rows.filter(x => x.id == 1)`,
		"`outer ${`inner == ${1}`} tail`",
		`value/* == ignored */`,
		`response //item`,
		`response/root/contains`,
		`response/root/within`,
		"`outer ${/}/.test('x') ? `inner == ${1}` : 'none'} tail`",
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

func TestMatchLiteralsAndXPathExecute(t *testing.T) {
	source := `Feature: parsing
Scenario: expressions
` + "* match `outer ${`inner == ${1}`} tail` == 'outer inner == 1 tail'" + `
* def number = 1
* match number/* == ignored */ == 1
* xml document = <root><contains>yes</contains></root>
* match document /root/contains == 'yes'
* match document //contains == 'yes'
`
	f, err := feature.Parse("parsing.feature", source)
	if err != nil {
		t.Fatal(err)
	}
	result, err := RunFile(t.Context(), f, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed() {
		t.Fatal(result.Scenarios[0].Error)
	}
}

func FuzzMatchQuotedOperand(f *testing.F) {
	for _, seed := range []string{"contains", "a == b", "quote \" and backslash \\", "line\nbreak", "${`nested`}", "/root/*"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		literal := strconv.Quote(text)
		lhs, mode, rhs, ok := splitMatch(literal + " == true")
		if !ok || lhs != literal || mode != match.Equals || rhs != "true" {
			t.Fatalf("split inside quoted operand: %q %v %q %v", lhs, mode, rhs, ok)
		}
	})
}
