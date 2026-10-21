// Paths such as response.users[*].id are evaluated in Go. They are not JavaScript.
// Object literals and #? predicates run in Sobek.
package script

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"uuid"

	"github.com/gi-runner/gi/internal/value"
	"github.com/grafana/sobek"
)

type VM struct {
	rt          *sobek.Runtime
	gate        chan struct{}
	sched       sync.Mutex
	parked      bool
	wake        chan struct{}
	jobs        []func()
	note        chan struct{}
	ctx         context.Context //nolint:containedctx // a retained call cancels with the caller that leased this runtime
	bindings    Bindings
	karate      *sobek.Object
	apiReady    bool
	custom      []string
	headerValue sobek.Value
	hookValues  map[string]sobek.Value
	vars        map[string]any
	root        string
	featureDir  string
	headerFn    sobek.Callable
	headerMap   map[string]any
	hooks       map[string]sobek.Callable
	funcs       map[sobek.Value]jsFunc
	retryN      int
	retryEvery  time.Duration
	retryUntil  string
}

func New() *VM {
	rt := sobek.New()
	vm := &VM{rt: rt, vars: map[string]any{}, gate: make(chan struct{}, 1), note: make(chan struct{}), ctx: context.Background()}
	_ = rt.Set("uuid", func(sobek.FunctionCall) sobek.Value {
		return rt.ToValue(uuid.New().String())
	})
	return vm
}

// Isolated seeds a new realm with this VM's variables and callbacks.
// Declarations and built-in changes in the child stay there. stop ends its lease.
func (v *VM) Isolated(ctx context.Context) (*VM, func(), error) {
	child := New()
	if err := child.adopt(v); err != nil {
		return nil, nil, err
	}
	linked, cancel := context.WithCancel(v.leasedContext(ctx))
	stop, err := child.Watch(linked)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	return child, func() {
		stop()
		cancel()
	}, nil
}

func (v *VM) leasedContext(ctx context.Context) context.Context {
	if leaseFrom(ctx) != nil || leaseFrom(v.ctx) == nil {
		return ctx
	}
	return context.WithValue(ctx, runtimeLease{}, leaseFrom(v.ctx))
}

func (v *VM) Set(name string, val any) {
	if hasJSFunc(val) {
		v.vars[name] = val
		_ = v.rt.Set(name, v.toJS(val))
		return
	}
	if value.IsAbsent(val) {
		delete(v.vars, name)
		_ = v.rt.Set(name, sobek.Undefined())
		return
	}
	if h, ok := val.(host); ok {
		v.vars[name] = h
		_ = v.rt.Set(name, h.v)
		return
	}
	val = value.Normalize(val)
	v.vars[name] = val
	_ = v.rt.Set(name, v.jsValue(val))
}

func (v *VM) Get(name string) (any, bool) {
	if h, ok := v.vars[name].(host); ok {
		return h, true
	}
	rv := v.rt.Get(name)
	if rv == nil || sobek.IsUndefined(rv) {
		delete(v.vars, name)
		return nil, false
	}
	exported := v.export(rv)
	v.vars[name] = exported
	return exported, true
}

func (v *VM) Vars() map[string]any {
	out := make(map[string]any, len(v.vars))
	for name := range v.vars {
		if val, ok := v.Get(name); ok {
			out[name] = val
		}
	}
	return out
}

func (v *VM) SetFunction(name string, fn func(call sobek.FunctionCall) sobek.Value) {
	_ = v.rt.Set(name, fn)
}

func (v *VM) ToValue(val any) sobek.Value {
	return v.toJS(value.Normalize(val))
}

func (v *VM) SetPath(path string, val any) error {
	segs, ok := parsePath(path)
	if !ok || len(segs) < 2 || segs[0].kind != segField {
		return errors.New("set requires a path such as user.name")
	}
	val = value.Normalize(val)
	rootName := segs[0].name
	root, exists := v.Get(rootName)
	if _, xml := root.(interface{ DataValue() any }); xml {
		return errors.New("XML mutation requires set with an XPath")
	}
	if !exists || root == nil || value.IsAbsent(root) {
		root = map[string]any{}
	}
	updated, err := writeSegs(root, segs[1:], val)
	if err != nil {
		return err
	}
	v.Set(rootName, updated)
	return nil
}

func (v *VM) RemovePath(path string) error {
	segs, ok := parsePath(path)
	if !ok || len(segs) < 2 || segs[0].kind != segField {
		return errors.New("remove requires a path such as user.password")
	}
	root, ok := v.Get(segs[0].name)
	if _, xml := root.(interface{ DataValue() any }); xml {
		return errors.New("XML mutation requires remove with an XPath")
	}
	if !ok {
		return fmt.Errorf("undefined variable %s", segs[0].name)
	}
	updated, err := removeSegs(root, segs[1:])
	if err != nil {
		return err
	}
	v.Set(segs[0].name, updated)
	return nil
}

func writeSegs(cur any, segs []seg, val any) (any, error) {
	if len(segs) == 0 {
		return val, nil
	}
	s := segs[0]
	last := len(segs) == 1
	switch s.kind {
	case segField:
		m, ok := value.AsMap(cur)
		if !ok || m == nil {
			m = map[string]any{}
		}
		if last {
			m[s.name] = val
			return m, nil
		}
		next, err := writeSegs(m[s.name], segs[1:], val)
		if err != nil {
			return nil, err
		}
		m[s.name] = next
		return m, nil
	case segIndex:
		if s.index < 0 || s.index > 1_000_000 {
			return nil, errors.New("set array index exceeds limit (1000000)")
		}
		arr, _ := value.AsSlice(cur)
		for len(arr) <= s.index {
			arr = append(arr, nil)
		}
		if last {
			arr[s.index] = val
			return arr, nil
		}
		child := arr[s.index]
		next, err := writeSegs(child, segs[1:], val)
		if err != nil {
			return nil, err
		}
		arr[s.index] = next
		return arr, nil
	case segStar:
		return nil, errors.New("set does not support [*]")
	default:
		return nil, errors.New("set path is not valid")
	}
}

func removeSegs(cur any, segs []seg) (any, error) {
	s := segs[0]
	last := len(segs) == 1
	switch s.kind {
	case segIndex:
		arr, ok := value.AsSlice(cur)
		if !ok {
			return nil, errors.New("remove index requires an array")
		}
		if s.index < 0 || s.index >= len(arr) {
			return cur, nil
		}
		if last {
			return append(arr[:s.index], arr[s.index+1:]...), nil
		}
		next, err := removeSegs(arr[s.index], segs[1:])
		if err != nil {
			return nil, err
		}
		arr[s.index] = next
		return arr, nil
	case segStar:
		return nil, errors.New("remove does not support [*]")
	case segField:
		m, ok := value.AsMap(cur)
		if !ok {
			return nil, fmt.Errorf("remove .%s requires an object", s.name)
		}
		if last {
			delete(m, s.name)
			return m, nil
		}
		child, ok := m[s.name]
		if !ok {
			return m, nil
		}
		next, err := removeSegs(child, segs[1:])
		if err != nil {
			return nil, err
		}
		m[s.name] = next
		return m, nil
	}
	return nil, errors.New("remove path is not valid")
}

func (v *VM) Eval(expr string) (any, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil, errors.New("empty expression")
	}
	if got, handled, err := v.advancedPath(expr); handled {
		return got, err
	}
	if expr != "true" && expr != "false" && expr != "null" {
		if segs, ok := parsePath(expr); ok {
			root := v.rt.Get(segs[0].name)
			if _, host := asHost(root); !host || len(segs) == 1 {
				return v.evalLive(segs)
			}
		}
	}
	val, err := v.run(rewriteEmbeds(expr))
	if err != nil {
		return nil, err
	}
	v.pull()
	if val == nil || sobek.IsUndefined(val) || sobek.IsNull(val) {
		return nil, nil //nolint:nilnil // null and undefined are values
	}
	if h, ok := asHost(val); ok {
		return h, nil
	}
	return v.export(val), nil
}

type host struct{ v sobek.Value }

func asHost(val sobek.Value) (host, bool) {
	obj, ok := val.(*sobek.Object)
	if !ok {
		return host{}, false
	}
	flag := obj.Get("__gi")
	if flag == nil || sobek.IsUndefined(flag) || sobek.IsNull(flag) || !flag.ToBoolean() {
		return host{}, false
	}
	return host{v: val}, true
}

// _ is the current value. $ is the match root.
func (v *VM) Truthy(expr string, self, root any) (bool, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return false, errors.New("empty predicate")
	}
	prevSelf := v.rt.Get("_")
	prevRoot := v.rt.Get("$")
	_ = v.rt.Set("_", v.jsValue(jsValue(self)))
	_ = v.rt.Set("$", v.jsValue(jsValue(root)))
	defer func() {
		if prevSelf == nil {
			_ = v.rt.Set("_", sobek.Undefined())
		} else {
			_ = v.rt.Set("_", prevSelf)
		}
		if prevRoot == nil {
			_ = v.rt.Set("$", sobek.Undefined())
		} else {
			_ = v.rt.Set("$", prevRoot)
		}
	}()
	val, err := v.run(rewriteEmbeds(expr))
	if err != nil {
		return false, err
	}
	v.pull()
	if val == nil {
		return false, nil
	}
	return val.ToBoolean(), nil
}

func jsValue(v any) any {
	if v == nil || value.IsAbsent(v) {
		return nil
	}
	return value.Normalize(v)
}

func (v *VM) evalLive(segs []seg) (any, error) {
	if segs[0].kind == segField {
		name := segs[0].name
		if h, ok := v.vars[name].(host); ok && len(segs) == 1 {
			return h, nil
		}
		if rv := v.rt.Get(name); rv != nil && !sobek.IsUndefined(rv) {
			root := v.export(rv)
			v.vars[name] = root
			if len(segs) == 1 {
				return root, nil
			}
			return evalPath(map[string]any{name: root}, segs)
		}
	}
	return evalPath(v.vars, segs)
}

func (v *VM) export(rv sobek.Value) any {
	if h, ok := asHost(rv); ok {
		return h
	}
	if sobek.IsNull(rv) {
		return nil
	}
	if f, ok := v.funcs[rv]; ok {
		return f
	}
	if _, ok := sobek.AssertFunction(rv); ok {
		return jsFunc{vm: v, v: rv}
	}
	return v.exportObject(rv, make(map[*sobek.Object]any))
}

func (v *VM) exportObject(rv sobek.Value, seen map[*sobek.Object]any) any {
	if rv == nil || sobek.IsUndefined(rv) || sobek.IsNull(rv) {
		return nil
	}
	if f, ok := v.funcs[rv]; ok {
		return f
	}
	if _, ok := sobek.AssertFunction(rv); ok {
		return jsFunc{vm: v, v: rv}
	}
	obj, ok := rv.(*sobek.Object)
	if !ok {
		return value.Normalize(rv.Export())
	}
	if previous, ok := seen[obj]; ok {
		return previous
	}
	switch raw := obj.Export().(type) {
	case map[string]any:
		out := make(map[string]any, len(raw))
		seen[obj] = out
		for key := range raw {
			out[key] = v.exportObject(obj.Get(key), seen)
		}
		return out
	case []any:
		out := make([]any, len(raw))
		seen[obj] = out
		for i := range out {
			out[i] = v.exportObject(obj.Get(strconv.Itoa(i)), seen)
		}
		return out
	default:
		return value.Normalize(raw)
	}
}

func (v *VM) pull() {
	for name, cur := range v.vars {
		if _, ok := cur.(host); ok {
			continue
		}
		rv := v.rt.Get(name)
		if rv == nil || sobek.IsUndefined(rv) {
			continue
		}
		v.vars[name] = v.export(rv)
	}
}

// Watch leases this VM for one scenario or retained-function execution. All
// runtime operations, including cleanup and exports, must finish before stop.
func (v *VM) Watch(ctx context.Context) (func(), error) {
	release, owned, err := v.acquire(ctx)
	if err != nil {
		return nil, err
	}
	if !owned {
		return release, nil
	}
	return v.arm(ctx, release), nil
}

func (v *VM) arm(ctx context.Context, release func()) func() { //nolint:contextcheck // stop restores the previous caller context
	previous := v.ctx
	v.ctx = context.WithValue(ctx, runtimeLease{}, &lease{vm: v, parent: leaseFrom(ctx)})
	done := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { v.rt.Interrupt(ctx.Err()); close(done) })
	return func() {
		if !stop() {
			<-done
		}
		v.rt.ClearInterrupt()
		if err := previous.Err(); err != nil {
			v.rt.Interrupt(err)
		}
		v.ctx = previous
		release()
		v.sched.Lock()
		v.changedLocked()
		v.sched.Unlock()
	}
}

func (v *VM) run(src string) (sobek.Value, error) {
	prog, err := programs.compile(src)
	if err != nil {
		return nil, errors.New(firstLine(err.Error()))
	}

	val, err := v.rt.RunProgram(prog)
	if err != nil {
		return nil, err
	}
	return val, nil
}

func firstLine(s string) string {
	if before, _, ok := strings.Cut(s, "\n"); ok {
		return before
	}
	return s
}

type segKind int

const (
	segField segKind = iota
	segIndex
	segStar
)

type seg struct {
	kind  segKind
	name  string
	index int
}

func parsePath(s string) ([]seg, bool) {
	if s == "" {
		return nil, false
	}
	if !isIdentStart(rune(s[0])) {
		return nil, false
	}
	j := 1
	for j < len(s) && isIdentCont(rune(s[j])) {
		j++
	}
	segs := []seg{{kind: segField, name: s[:j]}}
	i := j
	for i < len(s) {
		switch s[i] {
		case '.':
			i++
			if i >= len(s) || !isIdentStart(rune(s[i])) {
				return nil, false
			}
			j = i + 1
			for j < len(s) && isIdentCont(rune(s[j])) {
				j++
			}
			segs = append(segs, seg{kind: segField, name: s[i:j]})
			i = j
		case '[':
			if i+1 >= len(s) {
				return nil, false
			}
			if s[i+1] == '*' && i+2 < len(s) && s[i+2] == ']' {
				segs = append(segs, seg{kind: segStar})
				i += 3
				continue
			}
			if s[i+1] == '\'' || s[i+1] == '"' {
				q := s[i+1]
				k := i + 2
				var name strings.Builder
				for k < len(s) && s[k] != q {
					if s[k] == '\\' && k+1 < len(s) {
						name.WriteByte(s[k+1])
						k += 2
						continue
					}
					name.WriteByte(s[k])
					k++
				}
				if k >= len(s) || k+1 >= len(s) || s[k+1] != ']' {
					return nil, false
				}
				segs = append(segs, seg{kind: segField, name: name.String()})
				i = k + 2
				continue
			}
			k := i + 1
			if k >= len(s) || s[k] < '0' || s[k] > '9' {
				return nil, false
			}
			n := 0
			for k < len(s) && s[k] >= '0' && s[k] <= '9' {
				digit := int(s[k] - '0')
				if n > (int(^uint(0)>>1)-digit)/10 {
					return nil, false
				}
				n = n*10 + digit
				k++
			}
			if k >= len(s) || s[k] != ']' {
				return nil, false
			}
			segs = append(segs, seg{kind: segIndex, index: n})
			i = k + 1
		default:
			return nil, false
		}
	}
	return segs, true
}

func isIdentStart(r rune) bool {
	return r == '_' || unicode.IsLetter(r)
}

func isIdentCont(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

func evalPath(vars map[string]any, segs []seg) (any, error) {
	if len(segs) == 0 || segs[0].kind != segField {
		return nil, errors.New("invalid path")
	}
	v, ok := vars[segs[0].name]
	if !ok {
		return nil, fmt.Errorf("undefined variable %s", segs[0].name)
	}
	return walk(v, segs[1:])
}

func walk(v any, segs []seg) (any, error) {
	if len(segs) == 0 {
		return v, nil
	}
	if v == nil || value.IsAbsent(v) {
		return value.Absent{}, nil
	}
	s := segs[0]
	switch s.kind {
	case segField:
		m, ok := value.AsMap(v)
		if !ok {
			return nil, fmt.Errorf(".%s requires an object, got %s", s.name, value.Kind(v))
		}
		child, ok := m[s.name]
		if !ok {
			return value.Absent{}, nil
		}
		return walk(child, segs[1:])
	case segStar:
		arr, ok := value.AsSlice(v)
		if !ok {
			return nil, fmt.Errorf("[*] requires an array, got %s", value.Kind(v))
		}
		out := make([]any, len(arr))
		for i, el := range arr {
			got, err := walk(el, segs[1:])
			if err != nil {
				return nil, err
			}
			out[i] = got
		}
		return out, nil
	case segIndex:
		arr, ok := value.AsSlice(v)
		if !ok {
			return nil, fmt.Errorf("[%d] requires an array, got %s", s.index, value.Kind(v))
		}
		if s.index < 0 || s.index >= len(arr) {
			return value.Absent{}, nil
		}
		return walk(arr[s.index], segs[1:])
	default:
		return nil, errors.New("path is not valid")
	}
}

type part struct {
	text bool
	s    string
}

// '#(email)' becomes (email). 'Bearer #(token)' becomes ("Bearer " + (token)).
func rewriteEmbeds(src string) string {
	var b strings.Builder
	for i := 0; i < len(src); {
		c := src[i]
		if c == '\'' || c == '"' {
			end, parts, embeds, ok := scanString(src, i)
			if !ok {
				b.WriteByte(c)
				i++
				continue
			}
			if !embeds {
				b.WriteString(src[i:end])
			} else {
				b.WriteString(renderConcat(parts))
			}
			i = end
			continue
		}
		if c == '#' && i+1 < len(src) && src[i+1] == '(' {
			expr, next, ok := extractParen(src, i+1)
			if ok {
				b.WriteByte('(')
				b.WriteString(rewriteEmbeds(expr))
				b.WriteByte(')')
				i = next
				continue
			}
		}
		b.WriteByte(c)
		i++
	}
	return b.String()
}

func renderConcat(parts []part) string {
	if len(parts) == 1 && !parts[0].text {
		return "(" + rewriteEmbeds(parts[0].s) + ")"
	}
	var b strings.Builder
	b.WriteByte('(')
	for i, p := range parts {
		if i > 0 {
			b.WriteString(" + ")
		}
		if p.text {
			b.WriteString(quoteJS(p.s))
		} else {
			b.WriteByte('(')
			b.WriteString(rewriteEmbeds(p.s))
			b.WriteByte(')')
		}
	}
	b.WriteByte(')')
	return b.String()
}

func quoteJS(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\', '"':
			b.WriteByte('\\')
			b.WriteRune(r)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func scanString(src string, i int) (end int, parts []part, embeds bool, ok bool) {
	q := src[i]
	var text strings.Builder
	flush := func() {
		if text.Len() == 0 {
			return
		}
		parts = append(parts, part{text: true, s: text.String()})
		text.Reset()
	}
	for j := i + 1; j < len(src); {
		c := src[j]
		if c == '\\' && j+1 < len(src) {
			text.WriteByte(unescape(src[j+1]))
			j += 2
			continue
		}
		if c == q {
			flush()
			return j + 1, parts, embeds, true
		}
		if c == '#' && j+1 < len(src) && src[j+1] == '(' {
			expr, next, good := extractParen(src, j+1)
			if good {
				flush()
				parts = append(parts, part{s: expr})
				embeds = true
				j = next
				continue
			}
		}
		text.WriteByte(c)
		j++
	}
	return 0, nil, false, false
}

func unescape(c byte) byte {
	switch c {
	case 'n':
		return '\n'
	case 'r':
		return '\r'
	case 't':
		return '\t'
	default:
		return c
	}
}

func extractParen(src string, open int) (expr string, next int, ok bool) {
	if open >= len(src) || src[open] != '(' {
		return "", 0, false
	}
	depth := 0
	for i := open; i < len(src); i++ {
		c := src[i]
		switch c {
		case '\'', '"':
			end := skipString(src, i)
			if end < 0 {
				return "", 0, false
			}
			i = end - 1
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return src[open+1 : i], i + 1, true
			}
		}
	}
	return "", 0, false
}

func skipString(src string, i int) int {
	q := src[i]
	for j := i + 1; j < len(src); j++ {
		if src[j] == '\\' && j+1 < len(src) {
			j++
			continue
		}
		if src[j] == q {
			return j + 1
		}
	}
	return -1
}

func (v *VM) IsFunction(expr string) bool {
	val, err := v.run(expr)
	if err != nil {
		return false
	}
	_, ok := sobek.AssertFunction(val)
	return ok
}

func (v *VM) Invoke(expr string, arg any) (any, error) {
	val, err := v.run(expr)
	if err != nil {
		return nil, err
	}
	return v.invoke(val, arg)
}

// CallFile evaluates a JS helper once, invoking it if it returns a function.
// An object-valued helper is returned as data, as in Karate.
func (v *VM) CallFile(ref string, arg any) (any, error) {
	val, err := v.read(ref)
	if err != nil {
		return nil, err
	}
	if _, ok := sobek.AssertFunction(val); !ok {
		return v.Seal(v.export(val)), nil
	}
	got, err := v.invoke(val, arg)
	if err != nil {
		return nil, err
	}
	return v.Seal(got), nil
}

func (v *VM) invoke(val sobek.Value, arg any) (any, error) {
	fn, ok := sobek.AssertFunction(val)
	if !ok {
		return nil, errors.New("call target is not a function")
	}
	got, err := fn(sobek.Undefined(), v.rt.ToValue(arg))
	if err != nil {
		return nil, err
	}
	v.pull()
	if got == nil || sobek.IsUndefined(got) {
		return nil, nil //nolint:nilnil // JavaScript undefined is a valid result.
	}
	return v.export(got), nil
}
