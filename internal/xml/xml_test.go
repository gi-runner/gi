package xml

import (
	"strings"
	"testing"
)

func TestParseAndFormat(t *testing.T) {
	got, err := Parse(`<user id="1"><name>Ada</name><tag>math</tag><tag>poet</tag></user>`)
	if err != nil {
		t.Fatal(err)
	}
	root, ok := got.(map[string]any)["user"].(map[string]any)
	if !ok {
		t.Fatalf("%#v", got)
	}
	if root["@id"] != "1" || root["name"] != "Ada" {
		t.Fatalf("%#v", root)
	}
	tags, ok := root["tag"].([]any)
	if !ok || len(tags) != 2 || tags[0] != "math" {
		t.Fatalf("%#v", root["tag"])
	}
	out, err := Format(got)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `<name>Ada</name>`) || !strings.Contains(out, `id="1"`) {
		t.Fatal(out)
	}
}

func TestRejectsMultipleRootsAndOutsideText(t *testing.T) {
	for _, raw := range []string{`<a/><b/>`, `prefix<a/>`, `<a/>suffix`} {
		if _, err := Parse(raw); err == nil {
			t.Errorf("accepted invalid document %q", raw)
		}
	}
	if _, err := Parse("<!--before--><a/><!--after-->\n"); err != nil {
		t.Fatal(err)
	}
}

func TestFormattingRejectsInvalidNames(t *testing.T) {
	for _, name := range []string{"bad name", `x/><injected`, "1name", "x:y:z"} {
		if _, err := Format(map[string]any{name: "value"}); err == nil {
			t.Errorf("accepted invalid XML name %q", name)
		}
	}
	if _, err := Format(map[string]any{"root": map[string]any{"@bad name": "value"}}); err == nil {
		t.Fatal("accepted invalid attribute name")
	}
}

func FuzzDocument(f *testing.F) {
	for _, raw := range []string{`<r/>`, `<r xmlns:x="urn:x">before<x:b id="1">bold</x:b>after</r>`, `<broken>`, `<a/><b/>`} {
		f.Add(raw)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		if len(raw) > 16384 {
			t.Skip()
		}
		doc, err := ParseDocument(raw)
		if err != nil {
			return
		}
		if _, err := ParseDocument(doc.String()); err != nil {
			t.Fatalf("valid document did not round trip: %v", err)
		}
	})
}

func TestSelectedXMLFragmentRetainsInheritedNamespaces(t *testing.T) {
	doc, err := ParseDocument(`<root xmlns:x="urn:x"><branch><x:item>one</x:item></branch></root>`)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := doc.Query("/root/branch")
	if err != nil {
		t.Fatal(err)
	}
	fragment, ok := selected.(*Document)
	if !ok {
		t.Fatalf("expected document, got %T", selected)
	}
	if !strings.Contains(fragment.String(), `xmlns:x="urn:x"`) {
		t.Fatal("fragment lost namespace binding")
	}
	reparsed, err := ParseDocument(fragment.String())
	if err != nil {
		t.Fatal(err)
	}
	got, err := reparsed.Query("/branch/x:item")
	if err != nil || got != "one" {
		t.Fatalf("namespace query after reparse: %v (%v)", got, err)
	}
}
