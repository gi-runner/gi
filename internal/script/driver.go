package script

import (
	"errors"

	"github.com/grafana/sobek"
)

// Element is a locator, not a DOM node or a cross-runtime browser handle.
type Element struct{ Locator string }

var driverMethods = []string{"click", "input", "inputFile", "clear", "select", "focus", "scroll", "text", "html", "value", "attribute", "exists", "enabled", "locate", "locateAll", "waitFor", "waitForText", "waitForEnabled", "waitForUrl", "waitUntil", "script", "screenshot", "back", "forward", "refresh", "quit", "frame", "dialog", "dialogText", "intercept", "window"}

func driverGlobal(name string) bool {
	switch name {
	case "quit", "back", "forward", "refresh", "frame", "dialog", "dialogText", "intercept", "window":
		return false
	default:
		return true
	}
}

func (v *VM) driverCall(name string, args []any) sobek.Value {
	if v.bindings.Driver == nil {
		panic(v.rt.NewGoError(errors.New("driver is not available")))
	}
	got, err := v.bindings.Driver(name, args)
	if err != nil {
		panic(v.rt.NewGoError(err))
	}
	switch x := got.(type) {
	case Element:
		return v.elementObject(x.Locator)
	case []Element:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = v.elementObject(item.Locator)
		}
		return v.rt.ToValue(out)
	default:
		return v.ToValue(got)
	}
}
func (v *VM) driverFunction(name string, prefix []any) func(sobek.FunctionCall) sobek.Value {
	return func(call sobek.FunctionCall) sobek.Value {
		args := exportArgs(call.Arguments, prefix)
		return v.driverCall(name, args)
	}
}

// Global helpers stay available before a browser is opened. The driver object
// and its navigation methods are created only when JavaScript first reads it.
func (v *VM) bindDriver() {
	functions := make(map[string]sobek.Value)
	for _, name := range driverMethods {
		if driverGlobal(name) {
			fn := v.rt.ToValue(v.driverFunction(name, nil))
			functions[name] = fn
			_ = v.rt.Set(name, fn)
		}
	}
	set := func(val sobek.Value) {
		if err := v.rt.GlobalObject().DefineDataProperty("driver", val, sobek.FLAG_TRUE, sobek.FLAG_TRUE, sobek.FLAG_TRUE); err != nil {
			panic(v.rt.NewGoError(err))
		}
	}
	getter := v.rt.ToValue(func(sobek.FunctionCall) sobek.Value {
		obj := v.driverObject(functions)
		set(obj)
		return obj
	})
	setter := v.rt.ToValue(func(call sobek.FunctionCall) sobek.Value {
		set(call.Argument(0))
		return sobek.Undefined()
	})
	if err := v.rt.GlobalObject().DefineAccessorProperty("driver", getter, setter, sobek.FLAG_TRUE, sobek.FLAG_TRUE); err != nil {
		panic(v.rt.NewGoError(err))
	}
}

func (v *VM) driverObject(functions map[string]sobek.Value) *sobek.Object {
	obj := v.rt.NewObject()
	_ = obj.Set("__gi", true)
	for _, name := range driverMethods {
		fn := functions[name]
		if fn == nil {
			fn = v.rt.ToValue(v.driverFunction(name, nil))
		}
		_ = obj.Set(name, fn)
	}
	for _, property := range []struct{ name, action string }{{"url", "getUrl"}, {"title", "title"}} {
		getter := v.rt.ToValue(v.driverFunction(property.action, nil))
		setter := sobek.Undefined()
		if property.name == "url" {
			setter = v.rt.ToValue(v.driverFunction("open", nil))
		}
		if err := obj.DefineAccessorProperty(property.name, getter, setter, sobek.FLAG_TRUE, sobek.FLAG_TRUE); err != nil {
			panic(err)
		}
	}
	return obj
}
func (v *VM) elementObject(locator string) sobek.Value {
	obj := v.rt.NewObject()
	_ = obj.Set("__gi", true)
	_ = obj.Set("locator", locator)
	for _, name := range []string{"click", "input", "inputFile", "clear", "select", "focus", "scroll", "attribute", "script", "waitFor", "waitForText", "waitForEnabled"} {
		_ = obj.Set(name, v.driverFunction(name, []any{locator}))
	}
	for _, property := range []struct{ name, action string }{{"text", "text"}, {"html", "html"}, {"value", "value"}, {"present", "exists"}, {"enabled", "enabled"}} {
		getter := v.rt.ToValue(v.driverFunction(property.action, []any{locator}))
		setter := sobek.Undefined()
		if property.name == "value" {
			setter = v.rt.ToValue(v.driverFunction("value", []any{locator}))
		}
		if err := obj.DefineAccessorProperty(property.name, getter, setter, sobek.FLAG_TRUE, sobek.FLAG_TRUE); err != nil {
			panic(err)
		}
	}
	return obj
}
