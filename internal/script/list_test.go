package script_test

import (
	"testing"

	"github.com/gi-runner/gi/internal/script"
	"github.com/gi-runner/gi/internal/value"
)

func TestListHelpers(t *testing.T) {
	vm := script.New()
	vm.Bind(script.Bindings{})
	names, err := vm.Eval("gi.map([{name:'Ada'},{name:'Grace'}], function(x){ return x.name })")
	if err != nil {
		t.Fatal(err)
	}
	got, ok := value.AsSlice(names)
	if !ok || len(got) != 2 || got[0] != "Ada" || got[1] != "Grace" {
		t.Fatalf("%#v", names)
	}
	kept, err := vm.Eval("gi.filter([{age:36},{age:10}], function(x){ return x.age > 18 })")
	if err != nil {
		t.Fatal(err)
	}
	rows, ok := value.AsSlice(kept)
	if !ok || len(rows) != 1 {
		t.Fatalf("%#v", kept)
	}
	merged, err := vm.Eval("gi.merge({a:1, nest:{x:1}}, {b:2, nest:{y:2}})")
	if err != nil {
		t.Fatal(err)
	}
	m, ok := value.AsMap(merged)
	if !ok || m["a"] != int64(1) || m["b"] != int64(2) {
		t.Fatalf("%#v", merged)
	}
	nest, ok := value.AsMap(m["nest"])
	if !ok || nest["x"] != nil || nest["y"] != int64(2) {
		t.Fatalf("%#v", m["nest"])
	}
	appended, err := vm.Eval("gi.append([1], 2, 3)")
	if err != nil {
		t.Fatal(err)
	}
	list, ok := value.AsSlice(appended)
	if !ok || len(list) != 3 || list[2] != int64(3) {
		t.Fatalf("%#v", appended)
	}
}

func TestCollectionCompatibility(t *testing.T) {
	vm := script.New()
	vm.Bind(script.Bindings{})
	for _, expr := range []string{
		"karate.distinct([{a:1,b:2},{b:2,a:1}]).length === 1",
		"karate.filterKeys({a:1,b:2,c:3}, 'a', 'b').b === 2",
		"(function(){var n=0;var out=karate.sort([3,1,2],function(x,i){n++;return x});return n===3 && out[0]===1})()",
		"(function(){var list=[1];karate.appendTo(list,[2,3],4);return list.length===4 && list[3]===4})()",
	} {
		got, err := vm.Eval(expr)
		if err != nil || got != true {
			t.Errorf("%s: %v (%v)", expr, got, err)
		}
	}
}
