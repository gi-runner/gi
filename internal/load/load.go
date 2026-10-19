package load

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
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
	VUs        int
	Duration   time.Duration
	Warmup     time.Duration
	RPS        int
	Thresholds []string
	Vars       map[string]any
	Client     *http.Client
	Tag        string
	Timeout    time.Duration
	Env        string
	Suite      *suite.Suite
	Dial       func(context.Context, string, map[string]any) (engine.Link, error)
	NewPage    func(context.Context) (engine.Page, error)
}

type Result struct {
	Started          time.Time
	Elapsed          time.Duration
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
	if cfg.VUs < 1 {
		cfg.VUs = 1
	}
	if cfg.Duration <= 0 {
		cfg.Duration = 10 * time.Second
	}
	if cfg.Client == nil {
		cfg.Client = httpx.Pool(cfg.VUs)
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
	recordAt := started.Add(cfg.Warmup)
	end := recordAt.Add(cfg.Duration)
	var limiter *bucket
	if cfg.RPS > 0 {
		limiter = newBucket(runCtx, cfg.RPS)
	}
	var requests atomic.Int64
	var failures atomic.Int64
	var errMu sync.Mutex
	var runErrs []error
	locals := make([]*local, cfg.VUs)
	var wg sync.WaitGroup
	for i := range cfg.VUs {
		wg.Add(1)
		lu := &local{samples: map[string]*hist{}}
		locals[i] = lu
		go func(id int, lu *local) {
			defer wg.Done()
			iter := 0
			for {
				if runCtx.Err() != nil || !time.Now().Before(end) {
					return
				}
				iter++
				opt := engine.Options{
					Vars:     cfg.Vars,
					HTTP:     sender,
					LoadMode: true,
					VU:       id + 1,
					Iter:     iter,
					Tag:      cfg.Tag,
					Timeout:  cfg.Timeout,
					Env:      cfg.Env,
					Suite:    cfg.Suite,
					Dial:     cfg.Dial,
					NewPage:  cfg.NewPage,
					Jobs:     1,
					OnRequest: func(ex engine.Exchange) {
						now := time.Now()
						if now.Before(recordAt) || !now.Before(end) {
							return
						}
						requests.Add(1)
						lu.add(ex)
					},
				}
				if limiter != nil {
					opt.BeforeRequest = limiter.wait
				}
				iterCtx, cancel := context.WithDeadline(runCtx, end)
				res, err := engine.RunFile(iterCtx, f, opt)
				cancel()
				windowClosed := runCtx.Err() != nil || !time.Now().Before(end)
				if err != nil {
					if windowClosed && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
						return
					}
					errMu.Lock()
					runErrs = append(runErrs, err)
					errMu.Unlock()
					return
				}
				if windowClosed && !res.Passed() {
					// Keep assertion failures completed before cancellation. A later
					// in-flight request reaching the deadline must not hide them.
					res.Scenarios = slices.DeleteFunc(res.Scenarios, func(sc engine.ScenarioResult) bool { return sc.Canceled })
					if res.Passed() {
						return
					}
				}
				if time.Now().Before(recordAt) {
					continue
				}
				lu.iters++
				if !res.Passed() {
					lu.failIters++
					failures.Add(1)
					lu.keep(res)
				}
			}
		}(i, lu)
	}
	wg.Wait()
	elapsed := time.Since(recordAt)
	if elapsed < 0 {
		elapsed = time.Since(started)
	}
	merged := merge(locals, elapsed)
	merged.Started = started
	merged.Elapsed = elapsed
	merged.VUs = cfg.VUs
	merged.Requests = requests.Load()
	merged.FailedIterations = failures.Load()
	merged.Thresholds = evalThresholds(thresholds, merged)
	merged.Passed = merged.FailedIterations == 0
	for _, th := range merged.Thresholds {
		if !th.Passed {
			merged.Passed = false
		}
	}
	// A run that made no requests did not prove anything.
	if merged.Requests == 0 && cfg.Duration > 0 {
		merged.Passed = false
	}
	if err := ctx.Err(); err != nil {
		merged.Passed = false
		return merged, err
	}
	if err := errors.Join(runErrs...); err != nil {
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
	var iters, transport int64
	var failures []engine.ScenarioResult
	for _, l := range locals {
		iters += l.iters
		transport += l.transport
		all.merge(l.all)
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
	case "p50", "p95", "p99", "mean", "min", "max", "errorRate", "rps":
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
	case "rps":
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
		case "errorRate":
			actual = errRate
			shown = fmt.Sprintf("%.2f%%", errRate*100)
		case "rps":
			actual = rps
			shown = fmt.Sprintf("%.1f", rps)
		}
		ok := compare(actual, th.op, th.value)
		out = append(out, Threshold{Text: th.text, Passed: ok, Actual: shown})
	}
	return out
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
