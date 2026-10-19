package script_test

import (
	"testing"

	"github.com/gi-runner/gi/internal/script"
	"github.com/gi-runner/gi/internal/value"
)

func TestPathWildcard(t *testing.T) {
	vm := script.New()
	vm.Set("response", map[string]any{
		"users": []any{
			map[string]any{"id": "a"},
			map[string]any{"id": "b"},
		},
	})
	got, err := vm.Eval("response.users[*].id")
	if err != nil {
		t.Fatal(err)
	}
	ids, ok := value.AsSlice(got)
	if !ok || len(ids) != 2 || ids[0] != "a" || ids[1] != "b" {
		t.Fatalf("%#v", got)
	}
	missing, err := vm.Eval("response.users[0].missing")
	if err != nil {
		t.Fatal(err)
	}
	if !value.IsAbsent(missing) {
		t.Fatalf("want missing, got %#v", missing)
	}
}

func TestEmbedAndUUID(t *testing.T) {
	vm := script.New()
	vm.Set("email", "ada@example.com")
	got, err := vm.Eval("{ name: 'Ada', email: '#(email)', tags: ['math'] }")
	if err != nil {
		t.Fatal(err)
	}
	m, ok := value.AsMap(got)
	if !ok || m["email"] != "ada@example.com" || m["name"] != "Ada" {
		t.Fatalf("%#v", got)
	}
	id, err := vm.Eval("uuid()")
	if err != nil {
		t.Fatal(err)
	}
	s, _ := id.(string)
	if len(s) != 36 {
		t.Fatalf("uuid %q", s)
	}
}

func TestGetSeesAssignment(t *testing.T) {
	vm := script.New()
	vm.Set("n", int64(1))
	if _, err := vm.Eval("n = 2"); err != nil {
		t.Fatal(err)
	}
	got, ok := vm.Get("n")
	if !ok || got != int64(2) {
		t.Fatalf("Get = %#v ok=%v", got, ok)
	}
}

func TestHeaderPath(t *testing.T) {
	vm := script.New()
	vm.Set("responseHeaders", map[string]any{"Content-Type": "application/json"})
	got, err := vm.Eval("responseHeaders['Content-Type']")
	if err != nil {
		t.Fatal(err)
	}
	if got != "application/json" {
		t.Fatal(got)
	}
}
