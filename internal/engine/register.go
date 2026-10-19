package engine

import (
	"fmt"
	"maps"
	"sync"
)

var (
	helperMu sync.Mutex
	helpers  = map[string]func([]any) (any, error){}
)

// Register adds a function to the gi and karate objects for later scenarios.
// Built-in names are rejected. Unregister removes a previous registration.
func Register(name string, fn func([]any) (any, error)) error {
	if name == "" || fn == nil {
		return fmt.Errorf("helper %q requires a name and a function", name)
	}
	if !ident(name) || builtinHelper(name) {
		return fmt.Errorf("helper %q is not available for registration", name)
	}
	helperMu.Lock()
	helpers[name] = fn
	helperMu.Unlock()
	return nil
}

func Unregister(name string) {
	helperMu.Lock()
	delete(helpers, name)
	helperMu.Unlock()
}

func registeredHelpers() map[string]func([]any) (any, error) {
	helperMu.Lock()
	defer helperMu.Unlock()
	out := make(map[string]func([]any) (any, error), len(helpers))
	maps.Copy(out, helpers)
	return out
}

func builtinHelper(name string) bool {
	switch name {
	case "abort", "append", "appendTo", "call", "callonce", "callSingle", "channel", "config", "configure",
		"distinct", "embed", "env", "fail", "feature", "filter", "filterKeys", "forEach", "fromJson", "fromString",
		"get", "info", "jsonPath", "keysOf", "listen", "log", "lowerCase", "map", "mapWithKey", "merge", "pageText",
		"pause", "pretty", "range", "read", "readAsBytes", "readAsString", "remove", "repeat", "scenario",
		"set", "setup", "setupOnce", "signal", "sizeOf", "sort", "toBytes", "toJson", "toString", "typeOf",
		"urlDecode", "urlEncode", "uuid", "valuesOf", "waitForHttp", "waitForPort", "write", "xmlPath":
		return true
	default:
		return false
	}
}
