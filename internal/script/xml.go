package script

import (
	"errors"
	"slices"

	"github.com/gi-runner/gi/internal/value"
	"github.com/gi-runner/gi/internal/xml"
	"github.com/grafana/sobek"
)

// XML has a live read view in JavaScript and explicit XPath mutation. Returning
// read-only views avoids silently changing a detached map instead of the DOM.
type xmlView struct {
	vm   *VM
	doc  *xml.Document
	data any
}

func (x *xmlView) NativeValue() any {
	if x.doc != nil {
		return x.doc
	}
	return x.data
}
func (x *xmlView) dataValue() any {
	if x.doc != nil {
		return x.doc.DataValue()
	}
	return x.data
}
func (x *xmlView) Get(key string) sobek.Value {
	if x.doc != nil && key == "__xml" {
		return x.vm.rt.ToValue(true)
	}
	if x.doc != nil && key == "toString" {
		return x.vm.rt.ToValue(func() string { return x.doc.String() })
	}
	m, ok := value.AsMap(x.dataValue())
	if !ok {
		return nil
	}
	child, exists := m[key]
	if !exists {
		return nil
	}
	return x.vm.xmlReadValue(child)
}
func (x *xmlView) Set(string, sobek.Value) bool {
	panic(x.vm.rt.NewGoError(errors.New("XML mutation requires set with an XPath")))
}
func (x *xmlView) Delete(string) bool {
	panic(x.vm.rt.NewGoError(errors.New("XML mutation requires remove with an XPath")))
}
func (x *xmlView) Has(key string) bool {
	m, _ := value.AsMap(x.dataValue())
	_, ok := m[key]
	return ok
}
func (x *xmlView) Keys() []string {
	m, _ := value.AsMap(x.dataValue())
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

type xmlArrayView struct {
	vm   *VM
	data []any
}

func (x *xmlArrayView) NativeValue() any { return x.data }
func (x *xmlArrayView) Len() int         { return len(x.data) }
func (x *xmlArrayView) Get(i int) sobek.Value {
	if i < 0 || i >= len(x.data) {
		return sobek.Undefined()
	}
	return x.vm.xmlReadValue(x.data[i])
}
func (x *xmlArrayView) Set(int, sobek.Value) bool {
	panic(x.vm.rt.NewGoError(errors.New("XML mutation requires set with an XPath")))
}
func (x *xmlArrayView) SetLen(int) bool {
	panic(x.vm.rt.NewGoError(errors.New("XML mutation requires set/remove with an XPath")))
}

func (v *VM) xmlReadValue(data any) sobek.Value {
	switch x := data.(type) {
	case *xml.Document:
		return v.rt.NewDynamicObject(&xmlView{vm: v, doc: x})
	case map[string]any:
		return v.rt.NewDynamicObject(&xmlView{vm: v, data: x})
	case []any:
		return v.rt.NewDynamicArray(&xmlArrayView{vm: v, data: x})
	default:
		return v.rt.ToValue(data)
	}
}

func (v *VM) jsValue(data any) any {
	switch x := data.(type) {
	case *xml.Document:
		return v.xmlReadValue(x)
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[k] = v.jsValue(val)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			out[i] = v.jsValue(val)
		}
		return out
	default:
		return data
	}
}
