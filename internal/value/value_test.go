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

func TestClonePreservesCanonicalNumbers(t *testing.T) {
	for _, v := range []any{int(2), uint64(math.MaxUint64), float32(1.5), float64(2), math.NaN(), math.Inf(1)} {
		encoded, encodeErr := Marshal(v)
		got, err := Clone(v)
		if encodeErr != nil {
			if err == nil {
				t.Errorf("Clone(%v) accepted an invalid JSON number", v)
			}
			continue
		}
		want, parseErr := Parse(encoded)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		if got != want {
			t.Errorf("Clone(%T(%v)) = %T(%v), want %T(%v)", v, v, got, got, want, want)
		}
	}
}

func TestCompareNumbersPreservesPrecisionAndNonfinitePolicy(t *testing.T) {
	for _, test := range []struct {
		a, b  any
		order int
		ok    bool
	}{
		{int64(9007199254740993), float64(9007199254740992), 1, true},
		{float64(9007199254740992), int64(9007199254740993), -1, true},
		{int64(math.MaxInt64), int64(math.MinInt64), 1, true},
		{1.5, 1.25, 1, true}, {math.Copysign(0, -1), float64(0), 0, true},
		{math.NaN(), float64(1), 0, false}, {math.Inf(1), math.Inf(1), 0, false},
		{"1", int64(1), 0, false},
	} {
		order, ok := CompareNumbers(test.a, test.b)
		if order != test.order || ok != test.ok {
			t.Errorf("CompareNumbers(%v,%v) = %d,%v", test.a, test.b, order, ok)
		}
	}
}
