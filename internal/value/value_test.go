package value

import (
	"math"
	"strconv"
	"testing"
)

func TestJSONConsumesWholeDocument(t *testing.T) {
	for _, input := range []string{`{} {}`, `[] trailing`, `1 2`, `true false`} {
		if _, err := Parse([]byte(input)); err == nil {
			t.Errorf("accepted trailing data: %s", input)
		}
	}
	if _, err := Parse([]byte("{\"n\":9007199254740993} \n\t")); err != nil {
		t.Fatal(err)
	}
}

func TestIntegerBounds(t *testing.T) {
	limit := math.Ldexp(1, strconv.IntSize-1)
	for _, n := range []float64{1.5, math.NaN(), math.Inf(1), limit, -limit - 2048} {
		if _, ok := AsInt(n); ok {
			t.Errorf("accepted non-integer or out-of-range value %v", n)
		}
	}
	for _, n := range []float64{0, 42, -limit} {
		if got, ok := AsInt(n); !ok || float64(got) != n {
			t.Errorf("AsInt(%v) = %d, %v", n, got, ok)
		}
	}
	if got := Normalize(math.Ldexp(1, 63)); got != math.Ldexp(1, 63) {
		t.Errorf("positive float overflowed to %v", got)
	}
}

func TestNormalizeOwnsBytes(t *testing.T) {
	original := []byte("abc")
	copied, ok := Normalize(original).([]byte)
	if !ok {
		t.Fatal("normalized bytes changed type")
	}
	copied[0] = 'z'
	if string(original) != "abc" {
		t.Fatal("normalization retained mutable input")
	}
}
