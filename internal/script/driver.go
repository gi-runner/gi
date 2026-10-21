package script

import (
	"errors"

	"github.com/gi-runner/gi/internal/value"
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
		args := append([]any{}, prefix...)
		for _, a := range call.Arguments {
			args = append(args, value.Normalize(a.Export()))
		}
		return v.driverCall(name, args)
	}
}
func (v *VM) bindDriver() {
	obj := v.rt.NewObject()
	_ = obj.Set("__gi", true)
	for _, name := range driverMethods {
		fn := v.driverFunction(name, nil)
		_ = obj.Set(name, fn)
		// Navigation methods are available on driver; element helpers also have
		// the traditional Karate global form, e.g. waitFor('#name').input('Ada').
		if driverGlobal(name) {
			_ = v.rt.Set(name, fn)
		}
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
	_ = v.rt.Set("driver", obj)
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
