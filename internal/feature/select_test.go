package feature

import "testing"

func TestSelected(t *testing.T) {
	tags := []string{"@smoke", "@fast"}
	cases := []struct {
		expr string
		want bool
	}{
		{"@smoke", true},
		{"smoke", true},
		{"@slow", false},
		{"@smoke and @fast", true},
		{"@smoke and @slow", false},
		{"@slow or @fast", true},
		{"@smoke, @slow", true},
		{"not @slow", true},
		{"not @smoke", false},
		{"~@slow", true},
		{"@smoke and not @slow", true},
		{"(@slow or @fast) and @smoke", true},
		{"anyOf('@slow', '@fast')", true},
		{"allOf('@smoke', '@fast')", true},
		{"allOf('@smoke', '@slow')", false},
		{"anyOf('@smoke') && not('@slow')", true},
	}
	for _, tc := range cases {
		got, err := Selected(tags, tc.expr)
		if err != nil {
			t.Fatalf("%s: %v", tc.expr, err)
		}
		if got != tc.want {
			t.Errorf("%s = %v, want %v", tc.expr, got, tc.want)
		}
	}
	if _, err := Selected(tags, "@smoke and"); err == nil {
		t.Fatal("incomplete expression was accepted")
	}
}
