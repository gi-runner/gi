package xml

import (
	"strings"
	"testing"
)

func BenchmarkParseDocument(b *testing.B) {
	raw := "<items>" + strings.Repeat(`<item id="123"><name>Ada</name><active>true</active></item>`, 1000) + "</items>"
	b.ReportAllocs()
	for b.Loop() {
		if _, err := ParseDocument(raw); err != nil {
			b.Fatal(err)
		}
	}
}
