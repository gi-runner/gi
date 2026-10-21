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

func TestDeferredDriverAliasesAndOverrides(t *testing.T) {
	for _, expr := range []string{
		`driver.click === click && Object.keys(driver).includes('quit')`,
		`var original = click; click = 7; driver.click === original && click === 7`,
		`driver = {custom:true}; driver.custom && typeof click === 'function'`,
		`delete driver; typeof driver === 'undefined' && typeof click === 'function'`,
		`driver.extra = 7; driver.extra === 7`,
	} {
		t.Run(expr, func(t *testing.T) {
			v := New()
			if err := v.Bind(Bindings{}); err != nil {
				t.Fatal(err)
			}
			got, err := v.Eval(expr)
			if err != nil || got != true {
				t.Fatalf("%v: %v", got, err)
			}
		})
	}
}

func TestDeferredDriverUsesCurrentBindings(t *testing.T) {
	v := New()
	binding := func(label string) Bindings {
		return Bindings{Driver: func(string, []any) (any, error) { return label, nil }}
	}
	if err := v.Bind(binding("first")); err != nil {
		t.Fatal(err)
	}
	if err := v.Bind(binding("second")); err != nil {
		t.Fatal(err)
	}
	for _, expr := range []string{`driver.text('#a')`, `text('#a')`} {
		got, err := v.Eval(expr)
		if err != nil || got != "second" {
			t.Fatalf("%s = %v: %v", expr, got, err)
		}
	}
	if err := v.Bind(binding("third")); err != nil {
		t.Fatal(err)
	}
	got, err := v.Eval(`driver.text('#a')`)
	if err != nil || got != "third" {
		t.Fatalf("rebound driver: %v %v", got, err)
	}
}
