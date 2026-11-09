package script

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	osexec "os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/gi-runner/gi/internal/value"
	"github.com/grafana/sobek"
)

type procSpec struct {
	args    []string
	dir     string
	env     []string
	timeout time.Duration
	onOut   sobek.Callable
	onErr   sobek.Callable
	start   bool
}

type procLine struct {
	text   string
	stderr bool
}

// proc buffers output on reader goroutines; listeners run only on the VM goroutine.
type proc struct {
	vm      *VM
	spec    procSpec
	cmd     *osexec.Cmd
	mu      sync.Mutex
	out     strings.Builder
	errOut  strings.Builder
	pending []procLine
	notify  chan struct{}
	done    chan struct{}
	exit    int
	onOut   []sobek.Callable
	onErr   []sobek.Callable
	closed  bool
}

func (v *VM) bindProcess(obj *sobek.Object) {
	_ = obj.Set("exec", func(call sobek.FunctionCall) sobek.Value {
		p := v.newProc(call, "exec")
		defer p.kill()
		if err := p.begin(v.ctx); err != nil {
			panic(v.rt.NewGoError(err))
		}
		if _, err := p.wait(p.spec.timeout); err != nil {
			panic(v.rt.NewGoError(err))
		}
		return v.rt.ToValue(p.stdout())
	})
	_ = obj.Set("fork", func(call sobek.FunctionCall) sobek.Value {
		p := v.newProc(call, "fork")
		if p.spec.start {
			if err := p.begin(v.ctx); err != nil {
				panic(v.rt.NewGoError(err))
			}
		}
		return p.object()
	})
}

func (v *VM) newProc(call sobek.FunctionCall, name string) *proc {
	if v.bindings.NoProcess {
		panic(v.rt.NewGoError(fmt.Errorf("%s is not available in a mock", name)))
	}
	if len(call.Arguments) == 0 || missing(call.Argument(0)) {
		panic(v.rt.NewGoError(fmt.Errorf("%s needs a command line, an argument list, or options", name)))
	}
	spec, err := v.procSpec(call.Argument(0))
	if err != nil {
		panic(v.rt.NewGoError(fmt.Errorf("%s: %w", name, err)))
	}
	return &proc{vm: v, spec: spec, notify: make(chan struct{}, 1), done: make(chan struct{})}
}

func (v *VM) procSpec(arg sobek.Value) (procSpec, error) {
	spec := procSpec{start: true}
	if s, ok := arg.Export().(string); ok {
		spec.args = tokenize(s)
		return spec, spec.valid()
	}
	exported := value.Normalize(arg.Export())
	if list, ok := value.AsSlice(exported); ok {
		spec.args = texts(list)
		return spec, spec.valid()
	}
	obj := arg.ToObject(v.rt)
	opts, _ := value.AsMap(exported)
	switch a := opts["args"].(type) {
	case []any:
		spec.args = texts(a)
	case string:
		spec.args = tokenize(a)
	default:
		if line, ok := opts["line"].(string); ok {
			spec.args = tokenize(line)
		}
	}
	if shell, _ := opts["useShell"].(bool); shell && len(spec.args) > 0 {
		spec.args = shellWrap(strings.Join(spec.args, " "))
	}
	if dir, ok := opts["workingDir"].(string); ok {
		spec.dir = dir
	}
	if env, ok := value.AsMap(opts["env"]); ok {
		spec.env = os.Environ()
		for k, x := range env {
			spec.env = append(spec.env, k+"="+fmt.Sprint(x))
		}
	}
	if ms, ok := value.AsInt(opts["timeout"]); ok && ms > 0 {
		spec.timeout = time.Duration(ms) * time.Millisecond
	}
	if start, ok := opts["start"].(bool); ok {
		spec.start = start
	}
	spec.onOut, _ = sobek.AssertFunction(obj.Get("listener"))
	spec.onErr, _ = sobek.AssertFunction(obj.Get("errorListener"))
	return spec, spec.valid()
}

func texts(list []any) []string {
	out := make([]string, len(list))
	for i, x := range list {
		out[i] = fmt.Sprint(x)
	}
	return out
}

func (s procSpec) valid() error {
	if len(s.args) == 0 {
		return errors.New("the command is empty")
	}
	return nil
}

func shellWrap(line string) []string {
	if runtime.GOOS == "windows" {
		return []string{"cmd", "/c", line}
	}
	return []string{"sh", "-c", line}
}

// tokenize splits like a POSIX shell: quotes group, backslash escapes outside single quotes.
func tokenize(line string) []string {
	var out []string
	var cur strings.Builder
	var single, double, escaped, open bool
	for _, r := range line {
		switch {
		case escaped:
			cur.WriteRune(r)
			escaped = false
		case r == '\\' && !single:
			escaped, open = true, true
		case r == '\'' && !double:
			single, open = !single, true
		case r == '"' && !single:
			double, open = !double, true
		case unicode.IsSpace(r) && !single && !double:
			if cur.Len() > 0 {
				out = append(out, cur.String())
			}
			cur.Reset()
			open = false
		default:
			cur.WriteRune(r)
			open = true
		}
	}
	if open && cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

func (p *proc) begin(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cmd != nil {
		return nil
	}
	// A fork outlives the step that started it; scenario cleanup stops it.
	cmd := osexec.CommandContext(context.WithoutCancel(ctx), p.spec.args[0], p.spec.args[1:]...) // #nosec G204 -- test authors choose the command
	cmd.Dir = p.spec.dir
	cmd.Env = p.spec.env
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	p.cmd = cmd
	if p.spec.onOut != nil {
		p.onOut = append(p.onOut, p.spec.onOut)
	}
	if p.spec.onErr != nil {
		p.onErr = append(p.onErr, p.spec.onErr)
	}
	var readers sync.WaitGroup
	readers.Add(2)
	go p.read(stdout, false, &readers)
	go p.read(stderr, true, &readers)
	go func() {
		readers.Wait()
		err := cmd.Wait()
		p.mu.Lock()
		p.exit = exitCode(cmd, err)
		p.mu.Unlock()
		close(p.done)
	}()
	return nil
}

func exitCode(cmd *osexec.Cmd, err error) int {
	if cmd.ProcessState != nil {
		return cmd.ProcessState.ExitCode()
	}
	if err != nil {
		return -1
	}
	return 0
}

func (p *proc) read(r io.Reader, stderr bool, wg *sync.WaitGroup) {
	defer wg.Done()
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for sc.Scan() {
		line := sc.Text()
		p.mu.Lock()
		buf := &p.out
		if stderr {
			buf = &p.errOut
		}
		buf.WriteString(line)
		buf.WriteByte('\n')
		p.pending = append(p.pending, procLine{text: line, stderr: stderr})
		p.mu.Unlock()
		select {
		case p.notify <- struct{}{}:
		default:
		}
	}
}

// drain delivers buffered lines to listeners and returns the first line that satisfies want.
func (p *proc) drain(want sobek.Callable) (string, bool, error) {
	p.mu.Lock()
	lines := p.pending
	p.pending = nil
	onOut, onErr := p.onOut, p.onErr
	p.mu.Unlock()
	for i, l := range lines {
		listeners := onOut
		if l.stderr {
			listeners = onErr
		}
		arg := p.vm.rt.ToValue(l.text)
		for _, fn := range listeners {
			if _, err := fn(sobek.Undefined(), arg); err != nil {
				return "", false, err
			}
		}
		if want == nil || l.stderr {
			continue
		}
		got, err := want(sobek.Undefined(), arg)
		if err != nil {
			return "", false, err
		}
		if got.ToBoolean() {
			p.mu.Lock()
			p.pending = append(lines[i+1:len(lines):len(lines)], p.pending...)
			p.mu.Unlock()
			return l.text, true, nil
		}
	}
	return "", false, nil
}

// await parks until the process exits, the predicate matches, or the timeout ends.
func (p *proc) await(timeout time.Duration, want sobek.Callable) (string, bool, error) {
	if p.cmd == nil {
		return "", false, errors.New("process is not started")
	}
	var deadline <-chan time.Time
	if timeout > 0 {
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		deadline = timer.C
	}
	var line string
	var found bool
	err := p.vm.Park(func(wake <-chan struct{}) (bool, error) {
		var err error
		if line, found, err = p.drain(want); err != nil || found {
			return false, err
		}
		select {
		case <-p.done:
			line, found, err = p.drain(want)
			return false, err
		case <-p.notify:
			return true, nil
		case <-wake:
			return true, nil
		case <-deadline:
			return false, nil
		case <-p.vm.ctx.Done():
			return false, p.vm.ctx.Err()
		}
	})
	return line, found, err
}

func (p *proc) wait(timeout time.Duration) (int, error) {
	if _, _, err := p.await(timeout, nil); err != nil {
		return -1, err
	}
	select {
	case <-p.done:
	default:
		p.kill()
		return -1, fmt.Errorf("process timed out after %s", timeout)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.exit, nil
}

func (p *proc) stdout() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.out.String()
}

func (p *proc) kill() {
	p.mu.Lock()
	cmd, closed := p.cmd, p.closed
	p.closed = true
	p.mu.Unlock()
	if cmd == nil || closed {
		return
	}
	select {
	case <-p.done:
	default:
		_ = cmd.Process.Kill()
	}
}

// Close stops a process the scenario left running.
func (p *proc) Close() error {
	p.kill()
	if p.cmd != nil {
		<-p.done
	}
	return nil
}

func (p *proc) object() sobek.Value {
	v := p.vm
	if v.bindings.Track != nil {
		v.bindings.Track(p.Close)
	}
	obj := v.rt.NewObject()
	_ = obj.DefineDataProperty("__gi", v.rt.ToValue(true), sobek.FLAG_FALSE, sobek.FLAG_FALSE, sobek.FLAG_FALSE)
	getter := func(name string, fn func() any) {
		_ = obj.DefineAccessorProperty(name, v.rt.ToValue(func(sobek.FunctionCall) sobek.Value {
			if _, _, err := p.drain(nil); err != nil {
				panic(v.rt.NewGoError(err))
			}
			return v.rt.ToValue(fn())
		}), nil, sobek.FLAG_FALSE, sobek.FLAG_TRUE)
	}
	getter("stdOut", func() any { return p.stdout() })
	getter("stdErr", func() any { p.mu.Lock(); defer p.mu.Unlock(); return p.errOut.String() })
	getter("alive", func() any { return p.alive() })
	getter("pid", func() any {
		if p.cmd == nil {
			return -1
		}
		return p.cmd.Process.Pid
	})
	getter("exitCode", func() any {
		if p.alive() || p.cmd == nil {
			return nil
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		return p.exit
	})
	method := func(name string, fn func(sobek.FunctionCall) (any, error)) {
		_ = obj.Set(name, func(call sobek.FunctionCall) sobek.Value {
			got, err := fn(call)
			if err != nil {
				panic(v.rt.NewGoError(err))
			}
			if got == nil {
				return sobek.Undefined()
			}
			if sv, ok := got.(sobek.Value); ok {
				return sv
			}
			return v.rt.ToValue(got)
		})
	}
	method("start", func(sobek.FunctionCall) (any, error) { return obj, p.begin(v.ctx) })
	method("waitSync", func(call sobek.FunctionCall) (any, error) {
		return p.wait(argDuration(call.Argument(0), 0))
	})
	method("waitForOutput", func(call sobek.FunctionCall) (any, error) {
		want, ok := sobek.AssertFunction(call.Argument(0))
		if !ok {
			return nil, errors.New("waitForOutput requires a function argument")
		}
		line, found, err := p.await(argDuration(call.Argument(1), 0), want)
		if err != nil || !found {
			return nil, err
		}
		return line, nil
	})
	listen := func(name string, stderr bool) {
		method(name, func(call sobek.FunctionCall) (any, error) {
			fn, ok := sobek.AssertFunction(call.Argument(0))
			if !ok {
				return nil, fmt.Errorf("%s requires a function argument", name)
			}
			p.mu.Lock()
			if stderr {
				p.onErr = append(p.onErr, fn)
			} else {
				p.onOut = append(p.onOut, fn)
			}
			p.mu.Unlock()
			return obj, nil
		})
	}
	listen("onStdOut", false)
	listen("onStdErr", true)
	method("close", func(sobek.FunctionCall) (any, error) { return nil, p.Close() })
	method("signal", func(call sobek.FunctionCall) (any, error) {
		if v.bindings.Signal == nil {
			return nil, errors.New("signal is not available")
		}
		var val any
		if !missing(call.Argument(0)) {
			val = value.Normalize(call.Argument(0).Export())
		}
		return nil, v.bindings.Signal(val)
	})
	return obj
}

func (p *proc) alive() bool {
	if p.cmd == nil {
		return false
	}
	select {
	case <-p.done:
		return false
	default:
		return true
	}
}
