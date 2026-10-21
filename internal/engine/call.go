package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/gi-runner/gi/internal/feature"
	"github.com/gi-runner/gi/internal/script"
	"github.com/gi-runner/gi/internal/suite"
	"github.com/gi-runner/gi/internal/value"
)

func (e *exec) bind() error {
	root := filepath.Dir(e.file.Path)
	if e.opt.Suite != nil && e.opt.Suite.Root != "" {
		root = e.opt.Suite.Root
	}
	return e.vm.Bind(script.Bindings{
		Config:     e.configuration,
		Env:        e.opt.Env,
		Root:       root,
		FeatureDir: filepath.Dir(e.file.Path),
		Configure:  e.apply,
		CallSingle: e.callSingle,
		Setup:      e.setup,
		Channel:    e.openChannel,
		PageText:   e.pageText,
		Driver:     e.driverCall,
		Call:       func(call script.Call) (any, error) { return e.callValue(e.ctx, call) },
		Abort:      func() { e.aborted = true },
		Sleep:      e.sleep,
		Log:        e.logLine,
		Info:       e.info,
		Scenario:   e.scenarioInfo,
		Feature:    e.featureInfo,
		Write:      e.writeFile,
		WaitHTTP:   e.waitHTTP,
		WaitPort:   e.waitPort,
		Signal:     e.signal,
		Listen:     e.listen,
		Embed:      e.embed,
		Helpers:    registeredHelpers(),
	})
}

func (e *exec) evalSuite() error {
	if e.opt.skipConfig || e.opt.Suite == nil {
		return nil
	}
	if e.opt.Suite.Err != nil {
		return e.opt.Suite.Err
	}
	e.configuring = true
	defer func() { e.configuring = false }()
	for _, src := range []string{e.opt.Suite.Base, e.opt.Suite.Main, e.opt.Suite.Overlay} {
		if err := e.vm.EvalConfig(src); err != nil {
			return fmt.Errorf("config: %w", err)
		}
	}
	return nil
}

func (e *exec) callSingle(path string, arg any) (any, error) {
	target, query, hasQuery := strings.Cut(path, "?")
	resolved := e.resolveCall(target)
	key := resolved
	if hasQuery {
		key += "?" + query
	}
	if slices.Contains(e.stack, "single:"+key) {
		return nil, fmt.Errorf("cyclic callSingle: %s", path)
	}
	if slices.Contains(e.stack, resolved) {
		// Config may depend on the same bootstrap feature currently being called.
		if e.configuring {
			return map[string]any{}, nil
		}
		return nil, fmt.Errorf("cyclic callSingle: %s", path)
	}
	run := func(ctx context.Context) (any, error) { //nolint:contextcheck // Suite derives ctx from the caller with cache dependency metadata.
		if strings.HasSuffix(resolved, ".js") {
			previousCtx := e.ctx
			e.ctx = ctx
			defer func() { e.ctx = previousCtx }()
			previous := e.stack
			e.stack = append(append([]string{}, previous...), "single:"+key)
			defer func() { e.stack = previous }()
			return e.vm.CallFile(target, arg)
		}
		if arg != nil {
			if _, ok := value.AsMap(arg); !ok {
				return nil, errors.New("callSingle feature argument must be an object")
			}
		}
		result, steps, err := e.callFeature(ctx, resolved, arg, append(append([]string{}, e.stack...), resolved), -1, false)
		e.calls = append(e.calls, steps...)
		return result.vars, err
	}
	if e.opt.Suite == nil {
		return run(e.ctx)
	}
	return e.opt.Suite.DoSingle(e.ctx, key, run)
}

func (e *exec) call(ctx context.Context, expr string, once, shared bool) (any, error) {
	ref, argSrc, err := parseCall(expr)
	if err != nil {
		return nil, err
	}
	var arg any
	if argSrc != "" {
		arg, err = e.eval(argSrc)
		if err != nil {
			return nil, err
		}
	}
	if rest, ok := readExpression(expr); ok {
		rest = strings.TrimSpace(rest)
		inner, _, err := splitParen(rest)
		if err != nil {
			return nil, err
		}
		target, err := e.eval(inner)
		if err != nil {
			return nil, err
		}
		ref = value.Sprint(target)
	} else if !strings.HasPrefix(strings.TrimSpace(expr), "'") && !strings.HasPrefix(strings.TrimSpace(expr), "\"") {
		if _, ok := e.vm.Get(ref); ok {
			if e.vm.IsFunction(ref) {
				return e.callFunction(ctx, ref, arg, once, shared)
			}
			target, err := e.eval(ref)
			if err != nil {
				return nil, err
			}
			ref = value.Sprint(target)
		}
	}
	return e.callValue(ctx, script.Call{Path: ref, Arg: arg, Once: once, Shared: shared})
}

// callResult carries the callee's persistent HTTP settings independently of its
// variables. Request builders and cookie jars remain owned by the caller.
type callResult struct {
	vars       any
	state      session
	sharedVars map[string]any
	config     script.Configuration
	cookies    []cookieUpdate
}

func (r callResult) CloneCache() (any, error) {
	vars, err := value.Clone(r.vars)
	if err != nil {
		return nil, err
	}
	state := persistentSession(r.state)
	state.jar = nil // A cache must never share a scenario's cookie jar.
	shared, err := value.Clone(r.sharedVars)
	if err != nil {
		return nil, err
	}
	sharedVars, _ := value.AsMap(shared)
	config, err := r.config.Portable()
	if err != nil {
		return nil, err
	}
	return callResult{vars: vars, state: state, sharedVars: sharedVars, config: config, cookies: copyUpdates(r.cookies)}, nil
}

func (e *exec) resolveCall(ref string) string {
	if strings.HasPrefix(ref, "@") {
		return e.file.Path + ref
	}
	return suite.Resolve(e.root(), filepath.Dir(e.file.Path), ref)
}

func (e *exec) callValue(ctx context.Context, call script.Call) (any, error) {
	resolved := e.resolveCall(call.Path)
	if slices.Contains(e.stack, "call:"+resolved) {
		return nil, fmt.Errorf("cyclic feature call: %s", call.Path)
	}
	run := func(ctx context.Context) (any, error) { //nolint:contextcheck // Suite derives ctx from the caller with cache dependency metadata.
		if strings.HasSuffix(resolved, ".js") {
			previousCtx := e.ctx
			e.ctx = ctx
			defer func() { e.ctx = previousCtx }()
			previous := e.stack
			e.stack = append(append([]string{}, previous...), "call:"+resolved)
			defer func() { e.stack = previous }()
			got, err := e.vm.CallFile(call.Path, call.Arg)
			return callResult{vars: got, state: e.sess, config: e.vm.SealConfiguration(e.vm.Configuration())}, err
		}
		stack := append(append([]string{}, e.stack...), "call:"+resolved)
		if rows, ok := value.AsSlice(call.Arg); ok {
			out := make([]any, 0, len(rows))
			state := e.sess
			for i, row := range rows {
				if _, ok := value.AsMap(row); !ok {
					return nil, fmt.Errorf("call row %d must be an object", i)
				}
				result, steps, err := e.callFeature(ctx, resolved, row, stack, i, call.Shared)
				e.calls = append(e.calls, steps...)
				if err != nil {
					return nil, err
				}
				out = append(out, result.vars)
				state = result.state
			}
			return callResult{vars: out, state: state, config: e.vm.SealConfiguration(e.vm.Configuration())}, nil
		}
		if call.Arg != nil {
			if _, ok := value.AsMap(call.Arg); !ok {
				return nil, errors.New("feature call argument must be an object or a list of objects")
			}
		}
		result, steps, err := e.callFeature(ctx, resolved, call.Arg, stack, -1, call.Shared)
		e.calls = append(e.calls, steps...)
		return result, err
	}
	var got any
	var err error
	if call.Once && e.opt.Suite != nil {
		key := e.file.Path + "\x00" + resolved
		got, err = e.opt.Suite.DoOnce(ctx, key, run)
	} else {
		got, err = run(ctx)
	}
	if err != nil {
		return nil, err
	}
	result, ok := got.(callResult)
	if !ok {
		return nil, errors.New("invalid feature call cache entry")
	}
	if call.Shared {
		if err := e.vm.ApplyConfiguration(result.config); err != nil {
			return nil, err
		}
		jar := e.sess.jar
		e.sess = sessionFrom(Options{inherited: &result.state})
		e.sess.jar = jar
		e.sess.jar.replay(result.cookies)
		vars := result.sharedVars
		if vars == nil {
			vars, _ = value.AsMap(result.vars)
		}
		e.mergeCallVars(vars)
	}
	return result.vars, nil
}

func (e *exec) callFeature(ctx context.Context, path string, arg any, stack []string, loop int, sharedScope bool) (callResult, []StepResult, error) {
	if e.opt.Depth > 8 {
		return callResult{}, nil, errors.New("call nested too deeply")
	}
	path, selector, _ := strings.Cut(path, "@")
	path, lines, err := callLines(path)
	if err != nil {
		return callResult{}, nil, err
	}
	child, err := feature.ParseFile(path)
	if err != nil {
		return callResult{}, nil, err
	}
	vm := e.vm
	var stop func()
	if sharedScope {
		stop = e.vm.SharedScope()
	} else {
		isolated, release, err := e.vm.Isolated(ctx)
		if err != nil {
			return callResult{}, nil, err
		}
		stop = release
		vm = isolated
	}
	defer stop()
	opt := e.callOptions(stack)
	opt.vm = vm
	initial := e.vm.Vars()
	vars := maps.Clone(initial)
	if vars == nil {
		vars = map[string]any{}
	}
	if m, ok := arg.(map[string]any); ok {
		maps.Copy(vars, m)
	}
	vars["__arg"] = arg
	vars["__loop"] = int64(loop)
	opt.Vars = vars
	bindCallArg(arg, loop)(vm)
	include := func(sc feature.Scenario) (bool, error) {
		return !sc.IsSetup && selectedLine(sc, lines) && (selector == "" || feature.HasTag(sc.Tags, selector)) && feature.AllowsEnv(sc.Tags, e.opt.Env), nil
	}
	child, err = expand(ctx, child, opt, include)
	if err != nil {
		return callResult{}, nil, err
	}
	var called []StepResult
	out := map[string]any{}
	state := e.sess
	cookieStart := e.sess.jar.count()
	ran := false
	for _, sc := range child.Scenarios {
		if selected, _ := include(sc); !selected {
			continue
		}
		runOpt := opt
		runOpt.Vars = vars
		prepare := e.opt.Prepare
		runOpt.Prepare = func(vm *script.VM) {
			if prepare != nil {
				prepare(vm)
			}
			bindCallArg(arg, loop)(vm)
		}
		ran = true
		res := runScenario(ctx, child, sc, runOpt, func(childExec *exec) {
			state = childExec.sess
			if sharedScope {
				e.links = append(e.links, childExec.links...)
				childExec.links = nil
			}
			if sharedScope && childExec.page != nil {
				if e.page == nil {
					e.pageInherited = false
				}
				e.page = childExec.page
				childExec.pageInherited = true
			}
		})
		opt.inherited = &state
		called = append(called, res.Steps...)
		if !res.Passed {
			msg := res.Error
			if msg == "" {
				msg = "failed"
			}
			return callResult{}, called, fmt.Errorf("call %s: %s", filepath.Base(path), msg)
		}
		if res.Vars != nil {
			vars = res.Vars
		}
		for name := range res.Defined {
			out[name] = vars[name]
		}
	}
	if !ran {
		return callResult{}, called, fmt.Errorf("call %s: no scenarios selected", filepath.Base(path))
	}
	shared := maps.Clone(out)
	for name, val := range vars {
		if strings.HasPrefix(name, "__") {
			continue
		}
		if previous, exists := initial[name]; exists {
			before, beforeErr := value.Marshal(previous)
			after, afterErr := value.Marshal(val)
			// Unchanged runtime-owned values belong to the caller, not a cache.
			if beforeErr != nil || afterErr != nil || bytes.Equal(before, after) {
				continue
			}
		}
		shared[name] = val
	}
	captured := sealedMap(vm, shared)
	for name := range out {
		out[name] = captured[name]
	}
	return callResult{vars: out, state: state, sharedVars: captured, config: vm.SealConfiguration(vm.Configuration()), cookies: state.jar.updates(cookieStart)}, called, nil
}

func sealedMap(vm *script.VM, in map[string]any) map[string]any {
	if vm == nil || len(in) == 0 {
		return in
	}
	out, _ := vm.Seal(in).(map[string]any)
	return out
}

func (e *exec) callOptions(stack []string) Options {
	opt := e.opt
	opt.vm = e.vm
	opt.page = e.page
	opt.skipConfig = true
	inherited := e.sess
	opt.inherited = &inherited
	opt.Stdout = io.Discard
	opt.Timeout = e.sess.timeout
	opt.Depth++
	opt.SkipHooks = true
	opt.Stack = append([]string{}, stack...)
	opt.Prepare = nil
	opt.Jobs = 1
	return opt
}

func bindCallArg(arg any, loop int) func(*script.VM) {
	return func(vm *script.VM) {
		vm.Set("__arg", arg)
		vm.Set("__loop", int64(loop))
		m, ok := arg.(map[string]any)
		if !ok {
			return
		}
		for k, v := range m {
			vm.Set(k, v)
		}
	}
}

func (e *exec) root() string {
	if e.opt.Suite != nil && e.opt.Suite.Root != "" {
		return e.opt.Suite.Root
	}
	return filepath.Dir(e.file.Path)
}

func parseCall(expr string) (path, arg string, err error) {
	expr = strings.TrimSpace(expr)
	if rest, ok := readExpression(expr); ok {
		rest = strings.TrimSpace(rest)
		if !strings.HasPrefix(rest, "(") {
			return "", "", errors.New("call expects read('file') or a path")
		}
		inner, after, err := splitParen(rest)
		if err != nil {
			return "", "", err
		}
		path = unquote(strings.TrimSpace(inner))
		return path, strings.TrimSpace(after), err
	}
	return takeQuoted(expr)
}

func splitParen(s string) (inner, rest string, err error) {
	depth := 0
	in := byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if in != 0 {
			if c == '\\' && i+1 < len(s) {
				i++
				continue
			}
			if c == in {
				in = 0
			}
			continue
		}
		switch c {
		case '\'', '"':
			in = c
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return s[1:i], s[i+1:], nil
			}
		}
	}
	return "", "", errors.New("unclosed read(")
}

func takeQuoted(s string) (string, string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", "", errors.New("call expects a feature path")
	}
	if s[0] != '\'' && s[0] != '"' {
		path, arg, _ := strings.Cut(s, " ")
		return path, strings.TrimSpace(arg), nil
	}
	q := s[0]
	for i := 1; i < len(s); i++ {
		if s[i] == '\\' {
			i++
			continue
		}
		if s[i] == q {
			return s[1:i], strings.TrimSpace(s[i+1:]), nil
		}
	}
	return "", "", errors.New("unclosed path")
}

func unquote(s string) string {
	if len(s) < 2 {
		return s
	}
	if (s[0] == '\'' && s[len(s)-1] == '\'') || (s[0] == '"' && s[len(s)-1] == '"') {
		return s[1 : len(s)-1]
	}
	return s
}

func readExpression(expr string) (string, bool) {
	rest, ok := strings.CutPrefix(strings.TrimSpace(expr), "read")
	rest = strings.TrimSpace(rest)
	return rest, ok && strings.HasPrefix(rest, "(")
}

func (e *exec) mergeCallVars(vars map[string]any) {
	for name, v := range vars {
		if strings.HasPrefix(name, "__") {
			continue
		}
		e.vm.Set(name, v)
		e.defined[name] = true
	}
}
func (e *exec) callFunction(ctx context.Context, ref string, arg any, once, shared bool) (any, error) {
	run := func(callCtx context.Context) (any, error) { //nolint:contextcheck // Suite derives callCtx from the caller with cache dependency metadata.
		previous := e.ctx
		e.ctx = callCtx
		defer func() { e.ctx = previous }()
		return e.vm.Invoke(ref, arg)
	}
	var got any
	var err error
	if once && e.opt.Suite != nil {
		got, err = e.opt.Suite.DoOnce(ctx, e.file.Path+"\x00function:"+ref, run)
	} else {
		got, err = run(ctx)
	}
	if err != nil {
		return nil, err
	}
	if shared {
		if vars, ok := value.AsMap(got); ok {
			e.mergeCallVars(vars)
		}
	}
	return got, nil
}
