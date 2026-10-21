package load

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gi-runner/gi/internal/engine"
	"github.com/gi-runner/gi/internal/feature"
	"github.com/gi-runner/gi/internal/httpx"
	"github.com/gi-runner/gi/internal/suite"
)

type Config struct {
	VUs         int
	VUsTo       int
	UsersToSet  bool
	Rate        float64
	RateSet     bool
	RateTo      float64
	RateToSet   bool
	StageText   []string
	Stages      []Stage
	MaxInFlight int
	Duration    time.Duration
	Warmup      time.Duration
	RPS         int
	Thresholds  []string
	Vars        map[string]any
	Client      *http.Client
	Tag         string
	Timeout     time.Duration
	Env         string
	Suite       *suite.Suite
	Dial        func(context.Context, string, map[string]any) (engine.Link, error)
	NewPage     func(context.Context) (engine.Page, error)
}

type Result struct {
	Started          time.Time
	Elapsed          time.Duration
	Profile          string
	Open             bool
	VUs              int
	Iterations       int64
	FailedIterations int64
	Requests         int64
	TransportErrors  int64
	Endpoints        []Endpoint
	Thresholds       []Threshold
	Passed           bool
	Failures         []engine.ScenarioResult
	Overall          Stats
	Scheduled        Stats
	MissedStarts     int64
	LateStarts       int64
	MaxStartLag      time.Duration
	CPU              time.Duration
	RSS              uint64
	Goroutines       int
}

type Endpoint struct {
	Name   string
	Count  int64
	Errors int64
	Stats  Stats
	RPS    float64
}

type Stats struct {
	Min  time.Duration
	Max  time.Duration
	Mean time.Duration
	P50  time.Duration
	P95  time.Duration
	P99  time.Duration
}

type Threshold struct {
	Text   string
	Passed bool
	Actual string
}

func Run(ctx context.Context, f *feature.File, cfg Config) (Result, error) {
	cfg, err := Normalize(cfg)
	if err != nil {
		return Result{}, err
	}
	pl := newPlan(cfg.Warmup, cfg.Stages)
	pool := pl.maxUsers()
	if pl.rate() {
		pool = int(math.Ceil(pl.peakRate()))
	}
	if cfg.Client == nil {
		cfg.Client = httpx.Pool(pool)
		defer cfg.Client.CloseIdleConnections()
	}
	if cfg.Suite == nil {
		cfg.Suite = suite.Open(f.Path, cfg.Env)
	}
	sender := httpx.New(cfg.Client)
	defer sender.CloseIdleConnections()
	thresholds, err := ParseThresholds(cfg.Thresholds)
	if err != nil {
		return Result{}, err
	}
	runCtx, stop := context.WithCancel(ctx)
	defer stop()
	started := time.Now()
	s := &session{
		f:        f,
		cfg:      cfg,
		plan:     pl,
		sender:   sender,
		started:  started,
		recordAt: started.Add(cfg.Warmup),
		end:      started.Add(pl.window()),
		th:       thresholds,
	}
	if cfg.RPS > 0 {
		s.limiter = newBucket(runCtx, cfg.RPS)
	}
	watchCtx, stopWatch := context.WithCancel(ctx)
	var watchWG sync.WaitGroup
	watchWG.Go(func() { s.watch(watchCtx) })
	if pl.rate() {
		s.runOpen(runCtx)
	} else {
		s.runClosed(runCtx)
	}
	stopWatch()
	watchWG.Wait()
	return s.finish(ctx)
}

type session struct {
	f          *feature.File
	cfg        Config
	plan       plan
	sender     engine.Sender
	limiter    *bucket
	started    time.Time
	recordAt   time.Time
	end        time.Time
	th         []threshold
	requests   atomic.Int64
	failures   atomic.Int64
	inflight   atomic.Int64
	peak       atomic.Int64
	missed     atomic.Int64
	late       atomic.Int64
	maxLag     atomic.Int64
	cpu        time.Duration
	rss        uint64
	goroutines int
	errMu      sync.Mutex
	runErrs    []error
	accMu      sync.Mutex
	acc        local
}

type userSlot struct {
	stop context.CancelFunc
	done chan struct{}
}

func (s *session) runClosed(runCtx context.Context) {
	levels := s.plan.levels()
	n := s.plan.maxUsers()
	slots := make([]userSlot, n)
	var alive int
	var mu sync.Mutex
	var wg sync.WaitGroup
	ensure := func(target int) {
		if target < 0 {
			target = 0
		}
		if target > n {
			target = n
		}
		mu.Lock()
		defer mu.Unlock()
		for alive > target {
			alive--
			if slots[alive].stop != nil {
				slots[alive].stop()
			}
		}
		for alive < target {
			id := alive
			alive++
			s.startUser(runCtx, &wg, &mu, slots, id)
		}
	}
	wg.Go(func() {
		for _, lv := range levels {
			if err := waitUntil(runCtx, s.started.Add(lv.At)); err != nil {
				return
			}
			if lv.At > 0 && time.Now().After(s.end) {
				return
			}
			ensure(lv.N)
		}
	})
	wg.Wait()
}

func (s *session) startUser(runCtx context.Context, wg *sync.WaitGroup, mu *sync.Mutex, slots []userSlot, id int) {
	if slots[id].done != nil {
		done := slots[id].done
		mu.Unlock()
		select {
		case <-done:
		case <-runCtx.Done():
			mu.Lock()
			return
		}
		mu.Lock()
	}
	userCtx, stop := context.WithCancel(runCtx)
	done := make(chan struct{})
	slots[id] = userSlot{stop: stop, done: done}
	lu := &local{samples: map[string]*hist{}}
	wg.Go(func() {
		defer close(done)
		defer stop()
		defer s.fold(lu)
		for iter := 1; ; iter++ {
			if !s.once(runCtx, userCtx, lu, time.Time{}, id+1, iter) {
				return
			}
		}
	})
}

func (s *session) runOpen(runCtx context.Context) {
	var wg sync.WaitGroup
	for n := 0; ; n++ {
		off, ok := s.plan.due(n)
		if !ok {
			break
		}
		due := s.started.Add(off)
		if err := waitUntil(runCtx, due); err != nil {
			break
		}
		if time.Now().After(s.end) {
			break
		}
		lag := time.Since(due)
		measured := !due.Before(s.recordAt)
		if s.cfg.MaxInFlight > 0 && !s.reserve() {
			if measured {
				s.missed.Add(1)
				s.failures.Add(1)
			}
			continue
		}
		if measured {
			s.noteLag(lag)
		}
		id := n + 1
		wg.Go(func() {
			if s.cfg.MaxInFlight == 0 {
				cur := s.inflight.Add(1)
				defer s.inflight.Add(-1)
				s.notePeak(cur)
			} else {
				defer s.inflight.Add(-1)
			}
			lu := &local{samples: map[string]*hist{}}
			s.once(runCtx, runCtx, lu, due, id, 1)
			s.fold(lu)
		})
	}
	wg.Wait()
}

func (s *session) reserve() bool {
	maxN := int64(s.cfg.MaxInFlight)
	for {
		cur := s.inflight.Load()
		if cur >= maxN {
			return false
		}
		if s.inflight.CompareAndSwap(cur, cur+1) {
			s.notePeak(cur + 1)
			return true
		}
	}
}

func (s *session) notePeak(cur int64) {
	for {
		prev := s.peak.Load()
		if cur <= prev || s.peak.CompareAndSwap(prev, cur) {
			return
		}
	}
}

func (s *session) noteLag(lag time.Duration) {
	if lag > lateAfter {
		s.late.Add(1)
	}
	ns := max(lag.Nanoseconds(), 0)
	for {
		prev := s.maxLag.Load()
		if ns <= prev || s.maxLag.CompareAndSwap(prev, ns) {
			return
		}
	}
}

func (s *session) once(runCtx, userCtx context.Context, lu *local, due time.Time, id, iter int) bool {
	if runCtx.Err() != nil || userCtx.Err() != nil || !time.Now().Before(s.end) {
		return false
	}
	opt := engine.Options{
		Vars:     s.cfg.Vars,
		HTTP:     s.sender,
		LoadMode: true,
		VU:       id,
		Iter:     iter,
		Tag:      s.cfg.Tag,
		Timeout:  s.cfg.Timeout,
		Env:      s.cfg.Env,
		Suite:    s.cfg.Suite,
		Dial:     s.cfg.Dial,
		NewPage:  s.cfg.NewPage,
		Jobs:     1,
		OnRequest: func(ex engine.Exchange) {
			now := time.Now()
			if now.Before(s.recordAt) || !now.Before(s.end) {
				return
			}
			s.requests.Add(1)
			lu.add(ex)
			if !due.IsZero() {
				lu.addSched(now.Sub(due))
			}
		},
	}
	if s.limiter != nil {
		opt.BeforeRequest = s.limiter.wait
	}
	iterCtx, cancel := context.WithDeadline(userCtx, s.end)
	res, err := engine.RunFile(iterCtx, s.f, opt)
	cancel()
	stopped := userCtx.Err() != nil || !time.Now().Before(s.end)
	if err != nil {
		if stopped && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
			return false
		}
		s.errMu.Lock()
		s.runErrs = append(s.runErrs, err)
		s.errMu.Unlock()
		return false
	}
	if stopped && !res.Passed() {
		// Keep assertion failures completed before cancellation. A later
		// in-flight request reaching the deadline must not hide them.
		res.Scenarios = slices.DeleteFunc(res.Scenarios, func(sc engine.ScenarioResult) bool { return sc.Canceled })
		if res.Passed() {
			return false
		}
	}
	if time.Now().Before(s.recordAt) {
		return !stopped && runCtx.Err() == nil && time.Now().Before(s.end)
	}
	lu.iters++
	if !res.Passed() {
		lu.failIters++
		s.failures.Add(1)
		lu.keep(res)
	}
	return !stopped && runCtx.Err() == nil && time.Now().Before(s.end)
}

func (s *session) watch(ctx context.Context) {
	cpu0, _ := osUsage()
	var peakG atomic.Int64
	var peakHeap atomic.Uint64
	sample := func() {
		g := int64(runtime.NumGoroutine())
		for {
			prev := peakG.Load()
			if g <= prev || peakG.CompareAndSwap(prev, g) {
				break
			}
		}
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		for {
			prev := peakHeap.Load()
			if m.HeapAlloc <= prev || peakHeap.CompareAndSwap(prev, m.HeapAlloc) {
				break
			}
		}
	}
	sample()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			sample()
			cpu1, rss := osUsage()
			if cpu1 >= cpu0 {
				s.cpu = cpu1 - cpu0
			}
			if rss == 0 {
				rss = peakHeap.Load()
			}
			s.rss = rss
			s.goroutines = int(peakG.Load())
			return
		case <-ticker.C:
			sample()
		}
	}
}

func (s *session) finish(ctx context.Context) (Result, error) {
	elapsed := time.Since(s.recordAt)
	if elapsed < 0 {
		elapsed = time.Since(s.started)
	}
	merged := merge([]*local{&s.acc}, elapsed)
	merged.Started = s.started
	merged.Elapsed = elapsed
	merged.Profile = Describe(s.cfg)
	merged.Open = s.cfg.openLoop()
	if merged.Open {
		merged.VUs = int(s.peak.Load())
	} else {
		merged.VUs = s.plan.maxUsers()
	}
	merged.Iterations += s.missed.Load()
	merged.Requests = s.requests.Load()
	merged.FailedIterations = s.failures.Load()
	merged.MissedStarts = s.missed.Load()
	merged.LateStarts = s.late.Load()
	merged.MaxStartLag = time.Duration(s.maxLag.Load())
	merged.CPU = s.cpu
	merged.RSS = s.rss
	merged.Goroutines = s.goroutines
	merged.Thresholds = evalThresholds(s.th, merged)
	merged.Passed = merged.FailedIterations == 0
	for _, th := range merged.Thresholds {
		if !th.Passed {
			merged.Passed = false
		}
	}
	// A run that made no requests did not prove anything.
	if merged.Requests == 0 && s.cfg.Duration > 0 {
		merged.Passed = false
	}
	if err := ctx.Err(); err != nil {
		merged.Passed = false
		return merged, err
	}
	s.errMu.Lock()
	err := errors.Join(s.runErrs...)
	s.errMu.Unlock()
	if err != nil {
		merged.Passed = false
		return merged, err
	}
	return merged, nil
}

type local struct {
	mu        sync.Mutex
	samples   map[string]*hist
	errors    map[string]int64
	all       hist
	sched     hist
	iters     int64
	failIters int64
	transport int64
	failures  []engine.ScenarioResult
}

func (l *local) add(ex engine.Exchange) {
	l.mu.Lock()
	defer l.mu.Unlock()
	ns := ex.Duration.Nanoseconds()
	h := l.samples[ex.Endpoint]
	if h == nil {
		h = &hist{}
		l.samples[ex.Endpoint] = h
	}
	h.add(ns)
	l.all.add(ns)
	if ex.Error != "" {
		if l.errors == nil {
			l.errors = map[string]int64{}
		}
		l.errors[ex.Endpoint]++
		l.transport++
	}
}

func (l *local) addSched(d time.Duration) {
	if d < 0 {
		d = 0
	}
	l.mu.Lock()
	l.sched.add(d.Nanoseconds())
	l.mu.Unlock()
}

func (s *session) fold(lu *local) {
	s.accMu.Lock()
	s.acc.absorb(lu)
	s.accMu.Unlock()
}

func (dst *local) absorb(src *local) {
	dst.iters += src.iters
	dst.transport += src.transport
	dst.all.merge(src.all)
	dst.sched.merge(src.sched)
	if len(src.samples) > 0 && dst.samples == nil {
		dst.samples = map[string]*hist{}
	}
	for name, h := range src.samples {
		acc := dst.samples[name]
		if acc == nil {
			acc = &hist{}
			dst.samples[name] = acc
		}
		acc.merge(*h)
	}
	for name, n := range src.errors {
		if dst.errors == nil {
			dst.errors = map[string]int64{}
		}
		dst.errors[name] += n
	}
	for _, sc := range src.failures {
		if len(dst.failures) >= 20 {
			return
		}
		dst.failures = append(dst.failures, sc)
	}
}

func (l *local) keep(res engine.FileResult) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.failures) >= 8 {
		return
	}
	for _, sc := range res.Scenarios {
		if sc.Passed || sc.Skipped {
			continue
		}
		l.failures = append(l.failures, sc)
		if len(l.failures) >= 8 {
			return
		}
	}
}

func merge(locals []*local, elapsed time.Duration) Result {
	by := map[string]hist{}
	errN := map[string]int64{}
	var all hist
	var sched hist
	var iters, transport int64
	var failures []engine.ScenarioResult
	for _, l := range locals {
		if l == nil {
			continue
		}
		iters += l.iters
		transport += l.transport
		all.merge(l.all)
		sched.merge(l.sched)
		for k, s := range l.samples {
			h := by[k]
			h.merge(*s)
			by[k] = h
		}
		for k, n := range l.errors {
			errN[k] += n
		}
		failures = append(failures, l.failures...)
	}
	sec := elapsed.Seconds()
	if sec <= 0 {
		sec = 1
	}
	endpoints := make([]Endpoint, 0, len(by))
	for name, samples := range by {
		endpoints = append(endpoints, Endpoint{
			Name:   name,
			Count:  samples.n,
			Errors: errN[name],
			Stats:  samples.stats(),
			RPS:    float64(samples.n) / sec,
		})
	}
	sort.Slice(endpoints, func(i, j int) bool { return endpoints[i].Name < endpoints[j].Name })
	if len(failures) > 20 {
		failures = failures[:20]
	}
	return Result{
		Iterations:      iters,
		TransportErrors: transport,
		Endpoints:       endpoints,
		Failures:        failures,
		Overall:         all.stats(),
		Scheduled:       sched.stats(),
	}
}

type threshold struct {
	metric string
	op     string
	value  float64
	unit   string
	text   string
}

// ParseThresholds parses expressions such as "p95 < 200ms" and "errorRate < 1%".
func ParseThresholds(in []string) ([]threshold, error) {
	var out []threshold
	for _, raw := range in {
		th, err := parseThreshold(raw)
		if err != nil {
			return nil, err
		}
		out = append(out, th)
	}
	return out, nil
}

func parseThreshold(raw string) (threshold, error) {
	text := strings.TrimSpace(raw)
	fields := strings.Fields(text)
	if len(fields) != 3 {
		return threshold{}, fmt.Errorf("threshold %q must look like: p95 < 200ms", raw)
	}
	metric, op, rhs := fields[0], fields[1], fields[2]
	switch op {
	case "<", ">", "<=", ">=":
	default:
		return threshold{}, fmt.Errorf("threshold %q has unknown operator %s", raw, op)
	}
	switch metric {
	case "p50", "p95", "p99", "mean", "min", "max", "errorRate", "rps",
		"schedP50", "schedP95", "schedP99", "schedMean", "schedMin", "schedMax",
		"missed", "late":
	default:
		return threshold{}, fmt.Errorf("unknown threshold metric %q", metric)
	}
	unit := ""
	var value float64
	switch metric {
	case "errorRate":
		if before, ok := strings.CutSuffix(rhs, "%"); ok {
			n, err := strconv.ParseFloat(before, 64)
			if err != nil {
				return threshold{}, fmt.Errorf("threshold %q: %w", raw, err)
			}
			value = n / 100
			unit = "ratio"
		} else {
			n, err := strconv.ParseFloat(rhs, 64)
			if err != nil {
				return threshold{}, fmt.Errorf("threshold %q: %w", raw, err)
			}
			value = n
			unit = "ratio"
		}
	case "rps", "missed", "late":
		n, err := strconv.ParseFloat(rhs, 64)
		if err != nil {
			return threshold{}, fmt.Errorf("threshold %q: %w", raw, err)
		}
		value = n
	default:
		d, err := time.ParseDuration(rhs)
		if err != nil {
			return threshold{}, fmt.Errorf("threshold %q: %w", raw, err)
		}
		value = float64(d)
		unit = "duration"
	}
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return threshold{}, fmt.Errorf("threshold %q requires a finite nonnegative value", raw)
	}
	if metric == "errorRate" && value > 1 {
		return threshold{}, fmt.Errorf("threshold %q errorRate must be between 0 and 100%%", raw)
	}
	return threshold{metric: metric, op: op, value: value, unit: unit, text: text}, nil
}

func evalThresholds(ths []threshold, res Result) []Threshold {
	sec := res.Elapsed.Seconds()
	if sec <= 0 {
		sec = 1
	}
	rps := float64(res.Requests) / sec
	errRate := 0.0
	if res.Iterations > 0 {
		errRate = float64(res.FailedIterations) / float64(res.Iterations)
	}
	out := make([]Threshold, 0, len(ths))
	for _, th := range ths {
		actual := 0.0
		shown := ""
		switch th.metric {
		case "p50":
			actual = float64(res.Overall.P50)
			shown = res.Overall.P50.String()
		case "p95":
			actual = float64(res.Overall.P95)
			shown = res.Overall.P95.String()
		case "p99":
			actual = float64(res.Overall.P99)
			shown = res.Overall.P99.String()
		case "mean":
			actual = float64(res.Overall.Mean)
			shown = res.Overall.Mean.String()
		case "min":
			actual = float64(res.Overall.Min)
			shown = res.Overall.Min.String()
		case "max":
			actual = float64(res.Overall.Max)
			shown = res.Overall.Max.String()
		case "schedP50", "schedP95", "schedP99", "schedMean", "schedMin", "schedMax":
			d := schedStat(res.Scheduled, th.metric)
			actual = float64(d)
			shown = d.String()
		case "errorRate":
			actual = errRate
			shown = fmt.Sprintf("%.2f%%", errRate*100)
		case "rps":
			actual = rps
			shown = fmt.Sprintf("%.1f", rps)
		case "missed":
			actual = float64(res.MissedStarts)
			shown = strconv.FormatInt(res.MissedStarts, 10)
		case "late":
			actual = float64(res.LateStarts)
			shown = strconv.FormatInt(res.LateStarts, 10)
		}
		ok := compare(actual, th.op, th.value)
		out = append(out, Threshold{Text: th.text, Passed: ok, Actual: shown})
	}
	return out
}

func schedStat(st Stats, metric string) time.Duration {
	switch metric {
	case "schedP50":
		return st.P50
	case "schedP95":
		return st.P95
	case "schedP99":
		return st.P99
	case "schedMean":
		return st.Mean
	case "schedMin":
		return st.Min
	default:
		return st.Max
	}
}

func compare(actual float64, op string, want float64) bool {
	switch op {
	case "<":
		return actual < want
	case ">":
		return actual > want
	case "<=":
		return actual <= want
	case ">=":
		return actual >= want
	default:
		return false
	}
}

type bucket struct {
	ch chan struct{}
}

func newBucket(ctx context.Context, rps int) *bucket {
	if rps < 1 {
		rps = 1
	}
	b := &bucket{ch: make(chan struct{}, 1)}
	interval := time.Second / time.Duration(rps)
	if interval <= 0 {
		interval = time.Microsecond
	}
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				select {
				case b.ch <- struct{}{}:
				default:
				}
			}
		}
	}()
	return b
}

func (b *bucket) wait(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-b.ch:
		return nil
	}
}
