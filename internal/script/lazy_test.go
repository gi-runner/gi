package script

import "testing"

func TestDeferredAPIAliasesAndEnumeration(t *testing.T) {
	for _, expression := range []string{
		`gi === karate && karate.range(1,3).length === 3`,
		`Object.keys(karate).includes('range') && karate.info.name === 'new'`,
		`gi = {custom: true}; gi.custom && karate.range(1,2).length === 2`,
		`karate = {custom: true}; karate.custom && gi.range(1,2).length === 2`,
		`delete gi; karate.range(1,2).length === 2`,
	} {
		t.Run(expression, func(t *testing.T) {
			v := New()
			if err := v.Bind(Bindings{Env: "old", Info: func() map[string]any { return map[string]any{"name": "old"} }}); err != nil {
				t.Fatal(err)
			}
			if err := v.Bind(Bindings{Env: "new", Info: func() map[string]any { return map[string]any{"name": "new"} }}); err != nil {
				t.Fatal(err)
			}
			got, err := v.Eval(expression)
			if err != nil || got != true {
				t.Fatalf("%v: %v", got, err)
			}
		})
	}
}

func TestDeferredAPIRebindAndMutation(t *testing.T) {
	v := New()
	if err := v.Bind(Bindings{Env: "first"}); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Eval(`karate.marker = 7; karate.range(1,2)`); err != nil {
		t.Fatal(err)
	}
	if err := v.Bind(Bindings{Env: "second"}); err != nil {
		t.Fatal(err)
	}
	got, err := v.Eval(`gi.env === 'second' && gi.marker === 7 && gi === karate`)
	if err != nil || got != true {
		t.Fatalf("rebind: %v %v", got, err)
	}
	other := New()
	if err := other.Bind(Bindings{}); err != nil {
		t.Fatal(err)
	}
	got, err = other.Eval(`typeof karate.marker === 'undefined'`)
	if err != nil || got != true {
		t.Fatalf("leaked mutation: %v %v", got, err)
	}
}
