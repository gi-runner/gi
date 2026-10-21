package xml

import (
	"strings"
	"testing"

	"github.com/antchfx/xmlquery"
)

func FuzzSingleParseCompatibility(f *testing.F) {
	for _, raw := range []string{`<a/>`, `<a/><b/>`, `prefix<a/>`, `<a/>suffix`, `<r xmlns:x="urn:x"><x:b>text</x:b></r>`, `<!--before--><a/><!--after-->`, `<?xml version="1.0"?><a/>`} {
		f.Add(raw)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		if len(raw) > 16384 {
			t.Skip()
		}
		_, oldErr := Parse(raw)
		if oldErr == nil {
			_, oldErr = xmlquery.Parse(strings.NewReader(raw))
		}
		_, newErr := ParseDocument(raw)
		if (oldErr == nil) != (newErr == nil) {
			t.Fatalf("changed XML acceptance for %q: previous=%v current=%v", raw, oldErr, newErr)
		}
	})
}
