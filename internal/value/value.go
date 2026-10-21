// Package value is the JSON-shaped data model shared by the script runtime
// and the matcher. Missing object keys are Absent, which is distinct from null.
package value

import (
	"bytes"
	"cmp"
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"strconv"
	"strings"
)

// Absent is a path that does not exist. It is not nil and not JSON null.
type Absent struct{}

func (Absent) String() string { return "<missing>" }

func IsAbsent(v any) bool {
	_, ok := v.(Absent)
	return ok
}

// Normalize copies v into the small set of types the matcher understands:
// nil, bool, int64, float64, string, []byte, []any, and map[string]any.
// Runtime-owned values (such as JS callbacks) stay opaque; caches validate them
// separately rather than sharing them across scenarios.
func Normalize(v any) any {
	switch t := v.(type) {
	case interface{ Export() any }:
		return Normalize(t.Export())
	case interface{ NativeValue() any }:
		return Normalize(t.NativeValue())
	case nil, Absent, bool, string, int64:
		return t
	case int:
		return int64(t)
	case int8:
		return int64(t)
	case int16:
		return int64(t)
	case int32:
		return int64(t)
	case uint:
		return uintToNumber(uint64(t))
	case uint8:
		return int64(t)
	case uint16:
		return int64(t)
	case uint32:
		return int64(t)
	case uint64:
		return uintToNumber(t)
	case float32:
		return normalizeFloat(float64(t))
	case float64:
		return normalizeFloat(t)
	case json.Number:
		if i, err := t.Int64(); err == nil {
			return i
		}
		f, err := t.Float64()
		if err != nil {
			return string(t)
		}
		return normalizeFloat(f)
	case []byte:
		return bytes.Clone(t)
	case []any:
		out := make([]any, len(t))
		for i := range t {
			out[i] = Normalize(t[i])
		}
		return out
	case []string:
		out := make([]any, len(t))
		for i := range t {
			out[i] = t[i]
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, child := range t {
			out[k] = Normalize(child)
		}
		return out
	case map[string]string:
		out := make(map[string]any, len(t))
		for k, child := range t {
			out[k] = child
		}
		return out
	default:
		return t
	}

}

func uintToNumber(n uint64) any {
	if n > math.MaxInt64 {
		return float64(n)
	}
	return int64(n)
}

func normalizeFloat(f float64) any {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return f
	}
	if f == math.Trunc(f) && f >= math.MinInt64 && f < -float64(math.MinInt64) {
		return int64(f)
	}
	return f
}

func Kind(v any) string {
	if model, ok := v.(interface{ DataValue() any }); ok {
		return Kind(model.DataValue())
	}
	switch v.(type) {
	case nil:
		return "null"
	case Absent:
		return "missing"
	case bool:
		return "boolean"
	case string:
		return "string"
	case int64, float64:
		return "number"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	default:
		return fmt.Sprintf("%T", v)
	}
}

func IsNumber(v any) bool {
	switch v.(type) {
	case int64, float64:
		return true
	default:
		return false
	}
}

// AsInt accepts only finite, integral values representable by the platform int.
func AsInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		i := int(n)
		return i, int64(i) == n
	case float64:
		limit := math.Ldexp(1, strconv.IntSize-1)
		if math.IsNaN(n) || math.IsInf(n, 0) || n != math.Trunc(n) || n < -limit || n >= limit {
			return 0, false
		}
		return int(n), true
	default:
		return 0, false
	}
}

func AsMap(v any) (map[string]any, bool) {
	if model, ok := v.(interface{ DataValue() any }); ok {
		return AsMap(model.DataValue())
	}
	m, ok := v.(map[string]any)
	return m, ok
}

func AsSlice(v any) ([]any, bool) {
	a, ok := v.([]any)
	return a, ok
}

func NumbersEqual(a, b any) (equal, ok bool) {
	order, ok := CompareNumbers(a, b)
	return order == 0 && ok, ok
}

func CompareNumbers(a, b any) (int, bool) {
	switch x := a.(type) {
	case int64:
		if y, ok := b.(int64); ok {
			return cmp.Compare(x, y), true
		}
	case float64:
		if y, ok := b.(float64); ok {
			if math.IsNaN(x) || math.IsNaN(y) || math.IsInf(x, 0) || math.IsInf(y, 0) {
				return 0, false
			}
			return cmp.Compare(x, y), true
		}
	}
	ar, aok := rat(a)
	br, bok := rat(b)
	if !aok || !bok {
		return 0, false
	}
	return ar.Cmp(br), true
}

func rat(v any) (*big.Rat, bool) {
	switch n := v.(type) {
	case int64:
		return big.NewRat(n, 1), true
	case float64:
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return nil, false
		}
		r := new(big.Rat)
		r.SetFloat64(n)
		return r, true
	default:
		return nil, false
	}
}

func Format(v any) string {
	if model, ok := v.(interface {
		DataValue() any
		String() string
	}); ok {
		return model.String()
	}
	if IsAbsent(v) {
		return "<missing>"
	}
	if s, ok := v.(string); ok {
		return strconv.Quote(s)
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	if len(raw) > 80 && (bytes.Contains(raw, []byte{'{'}) || bytes.Contains(raw, []byte{'['})) {
		var buf bytes.Buffer
		if err := json.Indent(&buf, raw, "", "  "); err == nil {
			return buf.String()
		}
	}
	return string(raw)
}

func Sprint(v any) string {
	switch n := v.(type) {
	case string:
		return n
	case int64:
		return strconv.FormatInt(n, 10)
	case float64:
		return strconv.FormatFloat(n, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(n)
	case nil:
		return ""
	default:
		return fmt.Sprint(v)
	}
}

func Truncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "\n… truncated"
}

func Clone(v any) (any, error) {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for key, child := range x {
			copied, err := Clone(child)
			if err != nil {
				return nil, err
			}
			out[key] = copied
		}
		return out, nil
	case []any:
		out := make([]any, len(x))
		for i, child := range x {
			copied, err := Clone(child)
			if err != nil {
				return nil, err
			}
			out[i] = copied
		}
		return out, nil
	case []byte:
		return bytes.Clone(x), nil
	case nil, bool, string, int64:
		return x, nil
	}
	if model, ok := v.(interface{ CloneValue() (any, error) }); ok {
		return model.CloneValue()
	}
	b, err := Marshal(v)
	if err != nil {
		return nil, err
	}
	return Parse(b)
}

func Parse(b []byte) (any, error) {
	d := jsontext.NewDecoder(bytes.NewReader(b))
	v, err := readJSON(d)
	if err != nil {
		return nil, err
	}
	if _, err := d.ReadToken(); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("unexpected data after JSON value")
		}
		return nil, err
	}
	return v, nil
}

func readJSON(d *jsontext.Decoder) (any, error) {
	tok, err := d.ReadToken()
	if err != nil {
		return nil, err
	}
	switch tok.Kind() {
	case jsontext.KindNull:
		return nil, nil //nolint:nilnil // null is a value
	case jsontext.KindTrue:
		return true, nil
	case jsontext.KindFalse:
		return false, nil
	case jsontext.KindString:
		return tok.String(), nil
	case jsontext.KindNumber:
		return jsonNumber(tok.String()), nil
	case jsontext.KindBeginObject:
		m := map[string]any{}
		for d.PeekKind() != '}' {
			key, err := d.ReadToken()
			if err != nil {
				return nil, err
			}
			name := key.String()
			child, err := readJSON(d)
			if err != nil {
				return nil, err
			}
			m[name] = child
		}
		if _, err := d.ReadToken(); err != nil {
			return nil, err
		}
		return m, nil
	case jsontext.KindBeginArray:
		a := []any{}
		for d.PeekKind() != ']' {
			child, err := readJSON(d)
			if err != nil {
				return nil, err
			}
			a = append(a, child)
		}
		if _, err := d.ReadToken(); err != nil {
			return nil, err
		}
		return a, nil
	case jsontext.KindInvalid, jsontext.KindEndObject, jsontext.KindEndArray:
		return nil, errors.New("unsupported json token")
	default:
		return nil, errors.New("unsupported json token")
	}
}

func jsonNumber(raw string) any {
	if i, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return i
	}
	if !strings.ContainsAny(raw, ".eE") {
		return raw
	}
	f, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return raw
	}
	return normalizeFloat(f)
}

func Marshal(v any) ([]byte, error) {
	return jsonv2.Marshal(Normalize(v))
}

func JoinPath(base, next string) string {
	if base == "" || base == "$" {
		if strings.HasPrefix(next, "[") {
			return "$" + next
		}
		return "$." + next
	}
	if strings.HasPrefix(next, "[") {
		return base + next
	}
	return base + "." + next
}
