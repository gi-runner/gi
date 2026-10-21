// HTTP dialing stays behind Sender. This package owns scenario execution.
package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gi-runner/gi/internal/feature"
	"github.com/gi-runner/gi/internal/script"
	"github.com/gi-runner/gi/internal/suite"
	"github.com/gi-runner/gi/internal/value"
	"github.com/gi-runner/gi/internal/xml"
)

type Options struct {
	Vars           map[string]any
	HTTP           Sender
	Stdout         io.Writer
	Jobs           int
	Tag            string
	LoadMode       bool
	Mock           bool
	VU             int
	Iter           int
	Timeout        time.Duration
	OnRequest      func(Exchange)
	BeforeRequest  func(context.Context) error
	Prepare        func(*script.VM)
	Dial           func(context.Context, string, map[string]any) (Link, error)
	NewPage        func(context.Context) (Page, error)
	Depth          int
	Env            string
	Suite          *suite.Suite
	SkipHooks      bool
	Stack          []string
	inherited      *session
	vm             *script.VM
	page           Page
	skipConfig     bool
	reportDisabled bool
}

type Sender interface {
	Send(ctx context.Context, call Call) (Reply, error)
}

// Link carries only the operations a protocol actually implements.
type Link struct {
	Send    func(context.Context, any) error
	Receive func(context.Context) (any, error)
	Call    func(context.Context, string, any) (any, error)
	Open    func(context.Context, string) (Stream, error)
	Close   func() error
}

// Stream is one streaming call. CloseSend half-closes the client side.
// Close releases the call.
type Stream struct {
	Send      func(context.Context, any) error
	Receive   func(context.Context) (any, error)
	CloseSend func() error
	Close     func() error
}

type Page interface {
	Open(ctx context.Context, url string) error
	Click(ctx context.Context, selector string) error
	Fill(ctx context.Context, selector, text string) error
	Text(ctx context.Context, selector string) (string, error)
	Close() error
}

type Call struct {
	Method    string
	URL       string
	Header    map[string][]string
	Cookies   [][2]string
	Jar       http.CookieJar
	Body      []byte
	Follow    bool
	Transport TransportSettings
}

type Reply struct {
	URL      string
	Status   int
	Header   map[string][]string
	Body     []byte
	Duration time.Duration
}

type Exchange struct {
	Started         time.Time
	Method          string
	URL             string
	Status          int
	Duration        time.Duration
	RequestHeaders  map[string]string
	RequestBody     string
	ResponseHeaders map[string]string
	ResponseBody    string
	Error           string
	Endpoint        string
}

type StepResult struct {
	Started  time.Time
	Line     int
	Keyword  string
	Text     string
	Passed   bool
	Canceled bool
	Duration time.Duration
	Error    string
	Diff     string
	Label    string
	Output   string
	HTTP     *Exchange
	Calls    []StepResult
	Embeds   []Embed
}

// Embed is one report attachment produced by karate.embed or a screenshot.
type Embed struct {
	Name string
	Mime string
	Data []byte
}

type ScenarioResult struct {
	Name           string
	Line           int
	Tags           []string
	Passed         bool
	Canceled       bool
	Skipped        bool
	Miss           bool
	Duration       time.Duration
	Started        time.Time
	Steps          []StepResult
	Error          string
	Vars           map[string]any
	Defined        map[string]bool
	ReportDisabled bool
}

type FileResult struct {
	Path      string
	Name      string
	Scenarios []ScenarioResult
	Duration  time.Duration
	Started   time.Time
}

func (f FileResult) Passed() bool {
	for _, s := range f.Scenarios {
		if s.Skipped || s.Miss {
			continue
		}
		if !s.Passed {
			return false
		}
	}
	return true
}

func newExec(ctx context.Context, f *feature.File, opt Options) *exec {
	vm := opt.vm
	if vm == nil {
		vm = script.New()
	}
	return &exec{
		page: opt.page, pageInherited: opt.page != nil,
		file:    f,
		opt:     opt,
		vm:      vm,
		ctx:     ctx,
		defined: map[string]bool{},
		stack:   append([]string{}, opt.Stack...),
		sess:    sessionFrom(opt),
	}
}

func sessionFrom(opt Options) session {
	jar := newCookieStore()
	s := session{timeout: opt.Timeout, follow: true, jar: jar}
	if opt.inherited != nil {
		s = *opt.inherited
		s.jar = opt.inherited.jar.fork()
		s.headers = cloneHeader(s.headers)
		s.cookies = append([][2]string(nil), s.cookies...)
		s.paths = nil
		s.params = nil
		s.form = nil
		s.parts = nil
		s.body = nil
		s.hasBody = false
	}
	if s.timeout <= 0 {
		s.timeout = 30 * time.Second
	}

	return s
}

func cancelled(sc feature.Scenario, err error) ScenarioResult {
	return ScenarioResult{
		Name: sc.Name, Line: sc.Line, Tags: append([]string{}, sc.Tags...),
		Passed: false, Canceled: true, Error: err.Error(), Started: time.Now(),
	}
}

func failedFile(f *feature.File, err error) FileResult {
	return FileResult{
		Path: f.Path, Name: f.Name,
		Scenarios: []ScenarioResult{{Name: f.Name, Passed: false, Error: err.Error()}},
	}
}

// RunFiles runs every file. One file uses Jobs across its scenarios.
// Several files use Jobs across files, and each file stays in order.
// done, when set, is called once per file, one file at a time.
func RunFiles(ctx context.Context, files []*feature.File, opt Options, done func(FileResult)) ([]FileResult, error) {
	opt.Stdout = synchronizeWriter(opt.Stdout)
	if opt.Suite == nil && len(files) > 0 {
		opt.Suite = suite.Open(files[0].Path, opt.Env)
	}
	n := max(opt.Jobs, 1)
	if len(files) < 2 {
		opt.Jobs = n
		out := make([]FileResult, 0, len(files))
		var errs []error
		for _, f := range files {
			res, err := finishFile(ctx, f, opt)
			out = append(out, res)
			if done != nil {
				done(res)
			}
			if err != nil {
				errs = append(errs, err)
				if ctx.Err() != nil {
					break
				}
			}
		}
		return out, errors.Join(errs...)
	}
	opt.Jobs = 1
	results := make([]FileResult, len(files))
	errs := make([]error, len(files))
	var mu sync.Mutex
	parallel(n, len(files), func(i int) {
		res, err := finishFile(ctx, files[i], opt)
		results[i] = res
		errs[i] = err
		if done == nil {
			return
		}
		mu.Lock()
		done(res)
		mu.Unlock()
	})
	return results, errors.Join(errs...)
}

func finishFile(ctx context.Context, f *feature.File, opt Options) (FileResult, error) {
	if err := ctx.Err(); err != nil {
		return failedFile(f, err), err
	}
	res, err := RunFile(ctx, f, opt)
	if err != nil {
		return failedFile(f, err), err
	}
	return res, nil
}

func parallel(n, count int, fn func(int)) {
	if count == 0 {
		return
	}
	n = min(max(n, 1), count)
	if n == 1 {
		for i := range count {
			fn(i)
		}
		return
	}
	sem := make(chan struct{}, n)
	var wg sync.WaitGroup
	for i := range count {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			fn(i)
		}(i)
	}
	wg.Wait()
}

// Background steps are already inlined into each scenario.
func RunFile(ctx context.Context, f *feature.File, opt Options) (FileResult, error) {
	started := time.Now()
	opt.Stdout = synchronizeWriter(opt.Stdout)
	if opt.Suite == nil {
		opt.Suite = suite.Open(f.Path, opt.Env)
	}
	include := func(sc feature.Scenario) (bool, error) { return selected(sc, opt) }
	f, err := expand(ctx, f, opt, include)
	if err != nil {
		return FileResult{}, err
	}
	out := FileResult{Path: f.Path, Name: f.Name, Started: started}
	type job struct {
		i  int
		sc feature.Scenario
	}
	var jobs []job
	for _, sc := range f.Scenarios {
		if sc.IsSetup {
			continue
		}
		ok, err := selected(sc, opt)
		if err != nil {
			return FileResult{}, err
		}
		if !ok {
			out.Scenarios = append(out.Scenarios, ScenarioResult{Name: sc.Name, Line: sc.Line, Tags: sc.Tags, Skipped: true, Passed: true})
			continue
		}

		jobs = append(jobs, job{i: len(out.Scenarios), sc: sc})
		out.Scenarios = append(out.Scenarios, ScenarioResult{})
	}
	n := max(opt.Jobs, 1)
	runtimes := make([]*exec, len(jobs))
	var completed sync.Mutex
	last := -1
	remaining := make(map[int]int)
	outlineRun := make(map[int]int)
	for _, j := range jobs {
		if j.sc.OutlineID != 0 {
			remaining[j.sc.OutlineID]++
		}
	}
	parallel(n, len(jobs), func(i int) {
		j := jobs[i]
		var res ScenarioResult
		if err := ctx.Err(); err != nil {
			res = cancelled(j.sc, err)
		} else {
			release, err := opt.Suite.Acquire(ctx, j.sc.Tags)
			if err != nil {
				res = cancelled(j.sc, err)
			} else {
				res = runScenario(ctx, f, j.sc, opt, func(ex *exec) { runtimes[i] = ex })
				release()
			}
		}
		out.Scenarios[j.i] = res
		outlineHook := -1
		completed.Lock()
		if runtimes[i] != nil {
			last = i
			if j.sc.OutlineID != 0 {
				outlineRun[j.sc.OutlineID] = i
			}
		}
		if j.sc.OutlineID != 0 {
			remaining[j.sc.OutlineID]--
			if remaining[j.sc.OutlineID] == 0 {
				if idx, ok := outlineRun[j.sc.OutlineID]; ok {
					outlineHook = idx
				}
			}
		}
		completed.Unlock()
		if outlineHook >= 0 {
			fileHook(ctx, runtimes[outlineHook], "afterScenarioOutline", &out.Scenarios[jobs[outlineHook].i])
		}
	})
	if last >= 0 && runtimes[last] != nil {
		fileHook(ctx, runtimes[last], "afterFeature", &out.Scenarios[jobs[last].i])
	}
	out.Duration = time.Since(started)
	return out, nil
}

func RunScenario(ctx context.Context, f *feature.File, sc feature.Scenario, opt Options) ScenarioResult {
	return runScenario(ctx, f, sc, opt, nil)
}

func runScenario(ctx context.Context, f *feature.File, sc feature.Scenario, opt Options, finish func(*exec)) (res ScenarioResult) {
	started := time.Now()
	opt.reportDisabled = opt.reportDisabled || feature.HasTag(sc.Tags, "report=false")
	defer func() {
		res.Duration = time.Since(started)
		if feature.HasTag(sc.Tags, "fail") && !res.Canceled && !res.Miss {
			if res.Passed {
				res.Passed = false
				res.Error = "scenario tagged @fail passed unexpectedly"
			} else {
				res.Passed = true
				res.Error = ""
			}
		}
		res.ReportDisabled = opt.reportDisabled
		if res.ReportDisabled {
			res.Steps = nil
			if res.Error != "" {
				res.Error = "scenario failed (report details suppressed)"
			}
		}
	}()
	res = ScenarioResult{Name: sc.Name, Line: sc.Line, Tags: append([]string{}, sc.Tags...), Passed: true, Started: started}
	if err := ctx.Err(); err != nil {
		res.Passed = false
		res.Canceled = true
		res.Error = err.Error()
		return res
	}
	ex := newExec(ctx, f, opt)
	ex.scenario = &sc
	vm := ex.vm
	stop := func() {}
	if opt.vm == nil {
		var err error
		stop, err = vm.Watch(ctx)
		if err != nil {
			res.Passed = false
			res.Canceled = true
			res.Error = err.Error()
			return res
		}
	}
	defer func() {
		defer stop()
		if finish != nil {
			finish(ex)
		}
		if cerr := ex.release(); cerr != nil && res.Error == "" {
			res.Passed = false
			res.Error = cerr.Error()
		}
	}()
	if err := ex.bind(); err != nil {
		res.Passed = false
		res.Canceled = ctx.Err() != nil
		res.Error = err.Error()
		return res
	}
	if err := ex.evalSuite(); err != nil {
		res.Canceled = errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
		res.Passed = false
		res.Error = err.Error()
		return res
	}
	if opt.VU > 0 {
		vm.Set("vu", int64(opt.VU))
	} else {
		vm.Set("vu", int64(1))
	}
	if opt.Iter > 0 {
		vm.Set("iter", int64(opt.Iter))
	} else {
		vm.Set("iter", int64(1))
	}
	if opt.vm == nil {
		for k, v := range opt.Vars {
			vm.Set(k, v)
		}
	}
	if opt.Prepare != nil {
		opt.Prepare(vm)
	}
	if sc.Row != nil {
		row := make(map[string]any, len(sc.Row))
		for k, val := range sc.Row {
			if name, typed := strings.CutSuffix(k, "!"); typed {
				var err error
				val, err = vm.Eval(value.Sprint(val))
				if err != nil {
					res.Passed = false
					res.Error = fmt.Sprintf("example %s: %v", name, err)
					return res
				}
				k = name
			}
			row[k] = val
			vm.Set(k, val)
		}
		vm.Set("__row", row)
		vm.Set("__num", int64(sc.Num))
	}

	stopped := false
	if !opt.SkipHooks {
		if sr, ok := ex.hook("beforeScenario"); ok {
			res.Steps = append(res.Steps, sr)
			if !sr.Passed {
				res.Passed = false
				if res.Error == "" {
					res.Error = fmt.Sprintf("%s:%d: %s", f.Path, sc.Line, sr.Error)
				}
				stopped = !ex.sess.continueOnFail
			}
		}
	}
	if !stopped {
		for _, st := range sc.Steps {
			if ex.aborted {
				break
			}
			if ctx.Err() != nil {
				if res.Passed {
					res.Canceled = true
				}
				res.Passed = false
				res.Error = ctx.Err().Error()
				break
			}
			sr := ex.runStep(ctx, st)
			res.Steps = append(res.Steps, sr)
			if sr.Passed {
				continue
			}
			if res.Passed {
				res.Canceled = sr.Canceled
			}
			if !sr.Canceled {
				res.Canceled = false
			}
			res.Passed = false
			if sr.Error != "" {
				prefix := fmt.Sprintf("%s:%d", f.Path, st.Line)
				if st.Label != "" {
					res.Error = st.Label + "\n" + prefix + ": " + sr.Error
				} else {
					res.Error = prefix + ": " + sr.Error
				}
				if sr.Diff != "" {
					res.Error += "\n" + sr.Diff
				}
			}
			if ex.miss {
				res.Miss = true
				res.Passed = false
				break
			}
			proceed := ex.sess.continueOnFail
			if !opt.SkipHooks && !sr.Canceled {
				hookStart := time.Now()
				decision, hookErr := vm.StepFailure(map[string]any{"error": sr.Error, "scenarioName": sc.Name, "featureName": f.Name, "step": map[string]any{"line": st.Line, "text": st.Text, "prefix": st.Keyword}})
				if decision.Override {
					proceed = decision.Continue
				}
				if hookErr != nil || len(ex.embeds) > 0 || len(ex.calls) > 0 {
					hookStep := StepResult{Keyword: "*", Text: "onStepFailure", Passed: hookErr == nil, Duration: time.Since(hookStart), Calls: ex.calls, Embeds: ex.embeds}
					ex.calls = nil
					ex.embeds = nil
					if hookErr != nil {
						hookStep.Error = hookErr.Error()
					}
					res.Steps = append(res.Steps, hookStep)
				}
			}
			if !proceed {
				break
			}
		}
	}
	if !opt.SkipHooks {
		if sr, ok := ex.hook("afterScenario"); ok {
			res.Steps = append(res.Steps, sr)
			if !sr.Passed {
				res.Passed = false
				if res.Error == "" {
					res.Error = fmt.Sprintf("%s:%d: %s", f.Path, sc.Line, sr.Error)
				}
			}
		}
	}
	if err := ctx.Err(); err != nil {
		if res.Passed {
			res.Canceled = true
		}
		res.Passed = false
		if res.Error == "" {
			res.Error = err.Error()
		}
	}
	res.Vars = vm.Vars()
	if len(ex.defined) > 0 {
		res.Defined = make(map[string]bool, len(ex.defined))
		maps.Copy(res.Defined, ex.defined)
	}
	return res
}

type session struct {
	transport      TransportSettings
	jar            *cookieStore
	matchEachEmpty bool
	strictParsing  bool
	baseURL        string
	paths          []string
	params         url.Values
	headers        map[string][]string
	cookies        [][2]string
	form           url.Values
	parts          []part
	body           any
	hasBody        bool
	auth           auth
	timeout        time.Duration
	follow         bool
	continueOnFail bool
}

type exec struct {
	file          *feature.File
	opt           Options
	vm            *script.VM
	ctx           context.Context //nolint:containedctx // one exec is one scenario
	stack         []string
	defined       map[string]bool
	sess          session
	configuring   bool
	miss          bool
	page          Page
	pageInherited bool
	links         []Link
	calls         []StepResult
	embeds        []Embed
	scenario      *feature.Scenario
	aborted       bool
}

type missError struct{ reason string }

func (m *missError) Error() string { return m.reason }

func (e *exec) runStep(ctx context.Context, st feature.Step) StepResult {
	start := time.Now()
	sr := StepResult{Started: start, Line: st.Line, Keyword: st.Keyword, Text: st.Text, Label: st.Label, Passed: true}
	err := e.step(ctx, st, &sr)
	sr.Calls = e.calls
	sr.Embeds = e.embeds
	e.calls = nil
	e.embeds = nil
	sr.Duration = time.Since(start)
	if err != nil {
		sr.Passed = false
		sr.Canceled = errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
		sr.Error = err.Error()
		if _, ok := errors.AsType[*missError](err); ok {
			e.miss = true
		}
	}
	return sr
}

func (e *exec) step(ctx context.Context, st feature.Step, sr *StepResult) error {
	text := strings.TrimSpace(st.Text)
	doc := st.DocString
	fields := strings.SplitN(text, " ", 2)
	cmd := strings.ToLower(fields[0])
	rest := ""
	if len(fields) == 2 {
		rest = strings.TrimSpace(fields[1])
	}
	switch cmd {
	case "url":
		v, err := e.eval(orDoc(rest, doc))
		if err != nil {
			return err
		}
		e.sess.baseURL = value.Sprint(v)
		return nil
	case "path":
		args, err := e.evalArgs(orDoc(rest, doc))
		if err != nil {
			return err
		}
		var appendPath func(any)
		appendPath = func(v any) {
			if v == nil {
				return
			}
			if list, ok := value.AsSlice(v); ok {
				for _, item := range list {
					appendPath(item)
				}
				return
			}
			e.sess.paths = append(e.sess.paths, value.Sprint(v))
		}
		for _, arg := range args {
			appendPath(arg)
		}
		return nil
	case "param":
		name, expr, ok := splitAssign(rest)
		if !ok {
			return errors.New("param expects name = value")
		}
		v, err := e.eval(expr)
		if err != nil {
			return err
		}
		if e.sess.params == nil {
			e.sess.params = url.Values{}
		}
		setValues(e.sess.params, strings.TrimSpace(name), v)
		return nil
	case "params":
		v, err := e.eval(orDoc(rest, doc))
		if err != nil {
			return err
		}
		m, ok := value.AsMap(v)
		if !ok {
			return errors.New("params expects an object")
		}
		if e.sess.params == nil {
			e.sess.params = url.Values{}
		}
		for k, child := range m {
			setValues(e.sess.params, k, child)
		}
		return nil
	case "header":
		name, expr, ok := splitAssign(rest)
		if !ok {
			return errors.New("header expects Name = value")
		}
		v, err := e.eval(expr)
		if err != nil {
			return err
		}
		e.sess.headers = headerValues(e.sess.headers, strings.TrimSpace(name), v)
		return nil
	case "headers":
		v, err := e.eval(orDoc(rest, doc))
		if err != nil {
			return err
		}
		m, ok := value.AsMap(v)
		if !ok {
			return errors.New("headers expects an object")
		}
		for k, child := range m {
			e.sess.headers = headerValues(e.sess.headers, k, child)
		}
		return nil
	case "cookie":
		name, expr, ok := splitAssign(rest)
		if !ok {
			return errors.New("cookie expects name = value")
		}
		v, err := e.eval(expr)
		if err != nil {
			return err
		}
		e.sess.cookies = append(e.sess.cookies, [2]string{strings.TrimSpace(name), cookieText(v)})
		return nil
	case "cookies":
		return e.cookies(orDoc(rest, doc))
	case "form":
		return e.formStep(rest, doc)
	case "multipart":
		return e.multipart(rest, doc)
	case "request":
		v, err := e.eval(orDoc(rest, doc))
		if err != nil {
			return err
		}
		e.sess.body = v
		e.sess.hasBody = true
		return nil
	case "soap":
		action, ok := strings.CutPrefix(rest, "action")
		if !ok {
			return errors.New("soap expects action <expression>")
		}
		var v any
		if strings.TrimSpace(action) != "" {
			var err error
			v, err = e.eval(action)
			if err != nil {
				return err
			}
		}
		e.sess.headers = headerSet(e.sess.headers, "SOAPAction", value.Sprint(v))
		e.sess.headers = headerSet(e.sess.headers, "Content-Type", "text/xml")
		return e.method(ctx, "POST", sr)
	case "method":
		return e.method(ctx, rest, sr)
	case "status":
		v, err := e.eval(rest)
		if err != nil {
			return err
		}
		want, ok := value.AsInt(v)
		if !ok {
			return fmt.Errorf("status expects a number, got %s", value.Kind(v))
		}
		got, _ := e.vm.Get("responseStatus")
		have, ok := value.AsInt(got)
		if !ok {
			return errors.New("no response yet")
		}
		if have != want {
			return fmt.Errorf("status %d != %d", have, want)
		}
		return nil
	case "match":
		return e.match(rest, doc, sr)
	case "def":
		return e.def(ctx, rest, doc)
	case "eval":
		_, err := e.eval(orDoc(rest, doc))
		return err
	case "string", "bytes":
		return e.convertStep(cmd, rest, doc)
	case "text":
		return e.textStep(rest, doc)
	case "json":
		return e.jsonStep(rest, doc)
	case "yaml":
		return e.yamlStep(rest, doc)
	case "csv":
		return e.csvStep(rest, doc)
	case "xml":
		return e.xmlStep(rest, doc)
	case "xmlstring":
		return e.xmlStringStep(rest, doc)
	case "driver":
		return e.driverStep(ctx, orDoc(rest, doc))
	case "click":
		return e.clickStep(ctx, orDoc(rest, doc))
	case "input":
		return e.inputStep(ctx, rest)
	case "copy":
		return e.copyStep(rest)
	case "table":
		return e.tableStep(rest, st.Rows)
	case "replace":
		return e.replaceStep(rest, st.Rows)
	case "assert":
		return e.assertStep(orDoc(rest, doc))
	case "print":
		return e.print(orDoc(rest, doc), sr)
	case "configure":
		return e.configure(rest)
	case "set":
		if len(st.Rows) > 0 {
			return e.setTable(rest, st.Rows)
		}
		lhs, rhs, ok := splitAssign(rest)
		if !ok {
			return errors.New("set expects path = value")
		}
		if strings.TrimSpace(rhs) == "" {
			rhs = doc
		}
		v, err := e.eval(rhs)
		if err != nil {
			return err
		}
		if name, path, ok := splitXMLPath(strings.TrimSpace(lhs)); ok {
			return e.mutateXML(name, path, v, false)
		}
		return e.vm.SetPath(strings.TrimSpace(lhs), v)
	case "remove":
		path := strings.TrimSpace(orDoc(rest, doc))
		if name, xpath, ok := splitXMLPath(path); ok {
			return e.mutateXML(name, xpath, nil, true)
		}
		return e.vm.RemovePath(path)
	case "call", "callonce":
		_, err := e.call(ctx, orDoc(rest, doc), cmd == "callonce", true)
		return err
	case "retry":
		expr, ok := strings.CutPrefix(strings.TrimSpace(rest), "until ")
		if !ok {
			return errors.New("retry expects until <expression>")
		}
		e.vm.SetRetryUntil(strings.TrimSpace(expr))
		return nil
	case "methodis":
		return e.methodIs(rest)
	case "pathmatches":
		return e.pathMatches(rest)
	default:
		_, err := e.eval(orDoc(text, doc))
		return err
	}
}

func (e *exec) eval(expr string) (any, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil, errors.New("missing expression")
	}
	if expr == "$" || strings.HasPrefix(expr, "$.") || strings.HasPrefix(expr, "$[") {
		response, ok := e.vm.Get("response")
		if !ok {
			return nil, errors.New("no response yet")
		}
		if expr == "$" {
			return response, nil
		}
		return script.JSONPath(response, expr)
	}
	if strings.HasPrefix(expr, "<") {
		return xml.ParseDocument(expr)
	}
	if path, ok := strings.CutPrefix(expr, "get "); ok {
		return e.getPath(path)
	}
	return e.vm.Eval(expr)
}

func (e *exec) def(ctx context.Context, rest, doc string) error {
	name, rhs, ok := splitAssign(rest)
	if !ok {
		return errors.New("def expects name = expression")
	}
	name = strings.TrimSpace(name)
	if !ident(name) {
		return errors.New("def name must be an identifier")
	}
	rhs = strings.TrimSpace(rhs)
	if rhs == "" {
		rhs = doc
	}
	if rest, ok := strings.CutPrefix(rhs, "callonce "); ok {
		return e.defCall(ctx, name, rest, true)
	}
	if rest, ok := strings.CutPrefix(rhs, "call "); ok {
		return e.defCall(ctx, name, rest, false)
	}
	val, err := e.eval(rhs)
	if err != nil {
		return err
	}
	e.vm.Set(name, val)
	e.defined[name] = true
	return nil
}

func (e *exec) defCall(ctx context.Context, name, expr string, once bool) error {
	val, err := e.call(ctx, strings.TrimSpace(expr), once, false)
	if err != nil {
		return err
	}
	e.vm.Set(name, val)
	e.defined[name] = true
	return nil
}

func (e *exec) configure(rest string) error {
	name, expr, ok := splitAssign(rest)
	if !ok {
		return errors.New("configure expects name = value")
	}
	name = strings.TrimSpace(name)
	expr = strings.TrimSpace(expr)
	switch name {
	case "headers":
		return e.vm.ConfigureHeaders(expr)
	case "beforeScenario", "afterScenario", "afterFeature", "afterScenarioOutline", "onStepFailure":
		return e.vm.ConfigureHook(name, expr)
	}
	val, err := e.eval(expr)
	if err != nil {
		return err
	}
	return e.apply(name, val)
}

func (e *exec) apply(name string, val any) error {
	switch name {
	case "timeout", "readTimeout":
		n, ok := value.AsInt(val)
		if !ok || n <= 0 || int64(n) > math.MaxInt64/int64(time.Millisecond) {
			return errors.New("timeout expects positive milliseconds within the duration range")
		}
		e.sess.timeout = time.Duration(n) * time.Millisecond
	case "url":
		e.sess.baseURL = value.Sprint(val)
	case "matchEachEmptyAllowed", "strictResponseParsing":
		b, ok := val.(bool)
		if !ok {
			return fmt.Errorf("%s expects a boolean", name)
		}
		if name == "matchEachEmptyAllowed" {
			e.sess.matchEachEmpty = b
		} else {
			e.sess.strictParsing = b
		}
	case "ssl":
		return e.configureSSL(val)
	case "proxy":
		return e.configureProxy(val)
	case "connectTimeout":
		return e.configureConnectTimeout(val)
	case "followRedirects":
		b, ok := val.(bool)
		if !ok {
			return errors.New("followRedirects expects true or false")
		}
		e.sess.follow = b
	case "continueOnStepFailure":
		b, ok := val.(bool)
		if !ok {
			return errors.New("continueOnStepFailure expects true or false")
		}
		e.sess.continueOnFail = b
	case "retry":
		return e.vm.ConfigureRetry(val)
	case "auth":
		return e.setAuth(val)
	default:
		return fmt.Errorf("unknown configure %q", name)
	}
	return nil
}

func (e *exec) print(expr string, sr *StepResult) error {
	args, err := e.evalArgs(expr)
	if err != nil {
		return err
	}
	vals := make([]string, len(args))
	for i, v := range args {
		vals[i] = value.Format(v)
	}
	line := strings.Join(vals, " ")
	sr.Output = line
	if e.opt.Stdout != nil && !e.opt.LoadMode && !e.opt.reportDisabled {
		if _, err := fmt.Fprintln(e.opt.Stdout, line); err != nil {
			return fmt.Errorf("print: %w", err)
		}
	}
	return nil
}

func (e *exec) methodIs(rest string) error {
	if !e.opt.Mock {
		return errors.New("methodIs is only valid in gi mock")
	}
	want := strings.Trim(strings.TrimSpace(rest), `"'`)
	got, _ := e.vm.Get("method")
	if !strings.EqualFold(value.Sprint(got), want) {
		return &missError{reason: "method is " + value.Sprint(got)}
	}
	return nil
}

func (e *exec) pathMatches(rest string) error {
	if !e.opt.Mock {
		return errors.New("pathMatches is only valid in gi mock")
	}
	pattern, err := e.eval(rest)
	if err != nil {
		// Allow a bare path written without quotes: pathMatches /users/{id}
		pattern = strings.TrimSpace(rest)
	}
	got, _ := e.vm.Get("path")
	params, ok := MatchPath(value.Sprint(got), value.Sprint(pattern))
	if !ok {
		return &missError{reason: "path does not match " + value.Sprint(pattern)}
	}
	e.vm.Set("pathParams", params)
	return nil
}

// MatchPath matches literal segments and captures {name} path parameters.
func MatchPath(got, pattern string) (map[string]any, bool) {
	gs := splitPath(got)
	ps := splitPath(pattern)
	if len(gs) != len(ps) {
		return nil, false
	}
	params := map[string]any{}
	for i := range ps {
		if strings.HasPrefix(ps[i], "{") && strings.HasSuffix(ps[i], "}") {
			params[ps[i][1:len(ps[i])-1]] = gs[i]
			continue
		}
		if gs[i] != ps[i] {
			return nil, false
		}
	}
	return params, true
}

func splitPath(p string) []string {
	p = strings.Trim(p, "/")
	if p == "" {
		return []string{}
	}
	return strings.Split(p, "/")
}

func orDoc(rest, doc string) string {
	if strings.TrimSpace(rest) == "" {
		return doc
	}
	return rest
}

func ident(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (i > 0 && r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return true
}

func splitAssign(s string) (lhs, rhs string, ok bool) {
	for i := range topLevel(s) {
		if s[i] == '=' && (i+1 >= len(s) || s[i+1] != '=') && (i == 0 || !strings.ContainsRune("=!<>", rune(s[i-1]))) {
			return s[:i], s[i+1:], true
		}
	}
	return "", "", false
}

func (e *exec) evalArgs(src string) ([]any, error) {
	if strings.TrimSpace(src) == "" {
		return nil, errors.New("missing expression")
	}
	data, err := e.eval("[" + src + "]")
	if err != nil {
		return nil, err
	}
	args, ok := value.AsSlice(data)
	if !ok {
		return nil, errors.New("argument expression did not return a list")
	}
	return args, nil
}
