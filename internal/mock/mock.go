// methodIs, pathMatches, and a failed match skip the scenario instead of failing it.
package mock

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gi-runner/gi/internal/engine"
	"github.com/gi-runner/gi/internal/feature"
	"github.com/gi-runner/gi/internal/script"
	"github.com/gi-runner/gi/internal/value"
	"github.com/grafana/sobek"
)

func Handler(f *feature.File) http.Handler {
	st := &store{data: map[string]any{}}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	runtime, initErr := engine.NewMockRuntime(ctx, f, func(vm *script.VM) { st.bind(vm); bindMatchers(vm) })
	cancel()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if initErr != nil {
			write(w, http.StatusInternalServerError, map[string]any{"error": initErr.Error()})
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
		if err != nil {
			write(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		if len(body) > 1<<20 {
			write(w, http.StatusRequestEntityTooLarge, map[string]any{"error": "request body exceeds 1MiB"})
			return
		}
		headers := map[string]any{}
		for k, vs := range r.Header {
			headers[k] = append([]string(nil), vs...)
		}
		params := map[string]any{}
		for k, vs := range r.URL.Query() {
			params[k] = append([]string(nil), vs...)
		}
		cookies := map[string]any{}
		for _, c := range r.Cookies() {
			cookies[c.Name] = map[string]any{"name": c.Name, "value": c.Value}
		}
		vars := map[string]any{"method": r.Method, "path": r.URL.Path, "requestMethod": r.Method, "requestPath": r.URL.Path, "requestUri": r.URL.RequestURI(), "request": parseRequest(body), "requestBytes": body, "requestHeaders": headers, "requestParams": params, "requestCookies": cookies}
		res := runtime.Run(r.Context(), vars)
		if res.Miss {
			write(w, http.StatusNotFound, map[string]any{"error": "no mock scenario matched"})
			return
		}
		if !res.Passed {
			write(w, http.StatusInternalServerError, map[string]any{"error": res.Error})
			return
		}
		status := http.StatusOK
		if n, ok := value.AsInt(res.Vars["responseStatus"]); ok {
			status = n
		}
		if status < 100 || status > 599 {
			write(w, http.StatusInternalServerError, map[string]any{"error": "invalid responseStatus"})
			return
		}
		if h, ok := value.AsMap(res.Vars["responseHeaders"]); ok {
			for k, v := range h {
				if values, ok := value.AsSlice(v); ok {
					for _, x := range values {
						w.Header().Add(k, value.Sprint(x))
					}
				} else {
					w.Header().Set(k, value.Sprint(v))
				}
			}
		}
		if delay, ok := value.AsInt(res.Vars["responseDelay"]); ok && delay > 0 {
			timer := time.NewTimer(time.Duration(delay) * time.Millisecond)
			defer timer.Stop()
			select {
			case <-r.Context().Done():
				return
			case <-timer.C:
			}
		}
		write(w, status, res.Vars["response"])
	})
}

func bindMatchers(vm *script.VM) {
	vm.SetFunction("pathMatches", func(call sobek.FunctionCall) sobek.Value {
		path, _ := vm.Get("requestPath")
		params, ok := engine.MatchPath(value.Sprint(path), call.Argument(0).String())
		if ok {
			vm.Set("pathParams", params)
		}
		return vm.ToValue(ok)
	})
	vm.SetFunction("methodIs", func(call sobek.FunctionCall) sobek.Value {
		method, _ := vm.Get("requestMethod")
		return vm.ToValue(strings.EqualFold(value.Sprint(method), call.Argument(0).String()))
	})
	vm.SetFunction("paramValue", func(call sobek.FunctionCall) sobek.Value {
		params, _ := vm.Get("requestParams")
		m, _ := value.AsMap(params)
		list, _ := value.AsSlice(m[call.Argument(0).String()])
		if len(list) == 0 {
			return sobek.Null()
		}
		return vm.ToValue(list[0])
	})
	vm.SetFunction("paramExists", func(call sobek.FunctionCall) sobek.Value {
		params, _ := vm.Get("requestParams")
		m, _ := value.AsMap(params)
		_, ok := m[call.Argument(0).String()]
		return vm.ToValue(ok)
	})
}

func parseRequest(raw []byte) any {
	if len(raw) == 0 {
		return nil
	}
	if v, err := value.Parse(raw); err == nil {
		return v
	}
	return string(raw)
}

func write(w http.ResponseWriter, status int, v any) {
	if v == nil {
		if w.Header().Get("Content-Type") == "" {
			w.Header().Set("Content-Type", "application/json")
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte("null\n"))
		return
	}
	if s, ok := v.(string); ok {
		if w.Header().Get("Content-Type") == "" {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(s))
		return
	}
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "application/json")
	}
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		return
	}
}

type store struct {
	mu    sync.Mutex
	order []string
	data  map[string]any
}

func (s *store) bind(vm *script.VM) {
	vm.SetFunction("storeSet", func(call sobek.FunctionCall) sobek.Value {
		if len(call.Arguments) < 2 {
			return sobek.Null()
		}
		key := call.Arguments[0].String()
		val := value.Normalize(call.Arguments[1].Export())
		s.mu.Lock()
		if _, ok := s.data[key]; !ok {
			s.order = append(s.order, key)
		}
		s.data[key] = val
		s.mu.Unlock()
		return vm.ToValue(val)
	})
	vm.SetFunction("storeGet", func(call sobek.FunctionCall) sobek.Value {
		if len(call.Arguments) < 1 {
			return sobek.Null()
		}
		key := call.Arguments[0].String()
		s.mu.Lock()
		val, ok := s.data[key]
		s.mu.Unlock()
		if !ok {
			return sobek.Null()
		}
		return vm.ToValue(val)
	})
	vm.SetFunction("storeAll", func(call sobek.FunctionCall) sobek.Value {
		s.mu.Lock()
		out := make([]any, 0, len(s.order))
		for _, k := range s.order {
			out = append(out, s.data[k])
		}
		s.mu.Unlock()
		return vm.ToValue(out)
	})
}
