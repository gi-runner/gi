package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/gi-runner/gi/internal/adapters"
	"github.com/gi-runner/gi/internal/engine"
	"github.com/gi-runner/gi/internal/feature"
	"github.com/gi-runner/gi/internal/httpx"
	"github.com/gi-runner/gi/internal/load"
	"github.com/gi-runner/gi/internal/mock"
	"github.com/gi-runner/gi/internal/report"
	"github.com/gi-runner/gi/internal/suite"
)

const version = "0.2.0"

func Main(args []string) int {
	if len(args) == 0 {
		usage(os.Stdout)
		return 2
	}
	switch args[0] {
	case "test":
		return cmdTest(args[1:])
	case "load":
		return cmdLoad(args[1:])
	case "mock":
		return cmdMock(args[1:])
	case "version", "-v", "--version":
		fmt.Println("gi", version)
		return 0
	case "help", "-h", "--help":
		usage(os.Stdout)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", args[0])
		usage(os.Stderr)
		return 2
	}
}

func usage(w io.Writer) {
	_, _ = fmt.Fprint(w, `gi runs a feature file as a functional test or a load test.

Usage:
  gi test [files or directories] [flags]
  gi load [files or directories] [flags]
  gi mock <file.feature> [flags]
  gi version

Functional flags:
  --env name          loads gi-config-<env>.js and selects @env tags
  --var key=value     variable available to the feature (repeatable)
  --tag expr          @smoke, @a and @b, @a or @b, not @slow
  --jobs n            features to run at once (default: one per CPU; 1 keeps order)
  --timeout 30s       per-request timeout
  --report file.html  self-contained HTML report
  --junit file.xml    JUnit XML
  --json file.json    JSON report
  --cucumber file.json  Cucumber JSON
  --otel-endpoint url OTLP/HTTP JSON trace endpoint

Load flags, in addition to --var, --tag, --timeout, and the report flags:
  --vus n             virtual users, each looping the scenario (default 10)
  --vus-to n          ramp concurrent users from --vus to n
  --rate n            scenario starts per second; earlier starts keep running
  --rate-to n         ramp the start rate from --rate to n
  --stage spec        repeatable segment, for example users=1:40 duration=30s
  --max-inflight n    open-loop cap; a start past the cap is a missed start
  --duration 10s      measured window
  --warmup 0s         unmeasured time before the window
  --rps n             cap on HTTP calls, 0 means no cap
  --threshold 'p95 < 200ms'   repeatable; also errorRate and rps

Mock flags:
  --addr :18080

A feature can declare load defaults in comments:

  # gi-load: vus=10 duration=10s
  # gi-load: vus=1 vusTo=40 duration=30s
  # gi-load: rate=50 duration=30s
  # gi-load: rate=10 rateTo=200 duration=30s
  # gi-load: stage users=1:40 duration=30s
  # gi-load: stage users=40 duration=1m
  # gi-load: stage spike users=200 duration=10s
  # gi-load: maxInflight=100
  # gi-load: threshold p95 < 200ms
  # gi-load: threshold schedP95 < 1s
  # gi-load: threshold errorRate < 1%
`)
}

func cmdTest(args []string) int {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	vars := multi{}
	env := fs.String("env", "", "")
	tag := fs.String("tag", "", "")
	jobs := fs.Int("jobs", 0, "")
	timeout := fs.Duration("timeout", 30*time.Second, "")
	reportPath := fs.String("report", "", "")
	junitPath := fs.String("junit", "", "")
	jsonPath := fs.String("json", "", "")
	cucumberPath := fs.String("cucumber", "", "")
	otel := fs.String("otel-endpoint", "", "")
	fs.Var(&vars, "var", "")
	if err := fs.Parse(reorderFlags(args)); err != nil {
		usage(os.Stderr)
		return 2
	}
	files, err := discover(fs.Args())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	varmap, err := vars.Map()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	su := suite.Open(firstPath(files), *env)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	started := time.Now()
	n := *jobs
	if n < 1 {
		n = runtime.GOMAXPROCS(0)
	}
	sender := httpx.New(nil)
	defer sender.CloseIdleConnections()
	opt := engine.Options{
		Vars: varmap, HTTP: sender, Tag: *tag, Timeout: *timeout, Stdout: os.Stdout,
		Env: *env, Suite: su, Dial: adapters.Dial, NewPage: adapters.NewPage,
	}
	features, err := runFiles(ctx, files, opt, n)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	passed := true
	ran := 0
	for _, res := range features {
		for _, sc := range res.Scenarios {
			if !sc.Skipped {
				ran++
			}
		}
		if !res.Passed() {
			passed = false
		}
	}
	if ran == 0 {
		fmt.Fprintln(os.Stderr, "no scenarios ran")
		return 1
	}
	doc := report.Document{Mode: "functional", Title: "gi functional", Started: started, Duration: time.Since(started), Features: features}
	if err := emit(ctx, doc, *reportPath, *junitPath, *jsonPath, *cucumberPath, *otel); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if !passed {
		return 1
	}
	return 0
}

func cmdLoad(args []string) int {
	fs := flag.NewFlagSet("load", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	vars := multi{}
	ths := multi{}
	env := fs.String("env", "", "")
	tag := fs.String("tag", "", "")
	vus := fs.Int("vus", 0, "")
	vusTo := fs.Int("vus-to", 0, "")
	rate := fs.Float64("rate", 0, "")
	rateTo := fs.Float64("rate-to", 0, "")
	stages := multi{}
	maxIn := fs.Int("max-inflight", 0, "")
	duration := fs.Duration("duration", 0, "")
	warmup := fs.Duration("warmup", 0, "")
	rps := fs.Int("rps", 0, "")
	timeout := fs.Duration("timeout", 30*time.Second, "")
	reportPath := fs.String("report", "", "")
	junitPath := fs.String("junit", "", "")
	jsonPath := fs.String("json", "", "")
	cucumberPath := fs.String("cucumber", "", "")
	otel := fs.String("otel-endpoint", "", "")
	fs.Var(&vars, "var", "")
	fs.Var(&ths, "threshold", "")
	fs.Var(&stages, "stage", "")
	if err := fs.Parse(reorderFlags(args)); err != nil {
		usage(os.Stderr)
		return 2
	}
	files, err := discover(fs.Args())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	varmap, err := vars.Map()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	su := suite.Open(firstPath(files), *env)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	started := time.Now()
	passed := true
	var features []engine.FileResult
	var loads []load.Result
	for _, path := range files {
		f, err := feature.ParseFile(path)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		cfg := load.Config{
			VUs: f.Load.VUs, VUsTo: f.Load.VUsTo, UsersToSet: f.Load.UsersToSet,
			Rate: f.Load.Rate, RateSet: f.Load.RateSet, RateTo: f.Load.RateTo, RateToSet: f.Load.RateToSet,
			StageText: append([]string{}, f.Load.StageText...), MaxInFlight: f.Load.MaxInFlight,
			Duration: f.Load.Duration, Warmup: f.Load.Warmup, RPS: f.Load.RPS,
			Thresholds: append([]string{}, f.Load.Thresholds...),
			Vars:       varmap, Tag: *tag, Timeout: *timeout, Env: *env, Suite: su,
			Dial: adapters.Dial, NewPage: adapters.NewPage,
		}
		if err := applyLoadFlags(&cfg, loadOptions{
			fs: fs, vus: *vus, vusTo: *vusTo, rate: *rate, rateTo: *rateTo,
			stages: stages, maxIn: *maxIn, duration: *duration, warmup: *warmup, rps: *rps,
		}); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		cfg, err = load.Normalize(cfg)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		cfg.Thresholds = append(cfg.Thresholds, ths...)
		fmt.Printf("load %s  %s  duration=%s", f.Name, load.Describe(cfg), cfg.Duration)
		if cfg.Warmup > 0 {
			fmt.Printf("  warmup=%s", cfg.Warmup)
		}
		if cfg.RPS > 0 {
			fmt.Printf("  rps=%d", cfg.RPS)
		}
		fmt.Println()
		res, err := load.Run(ctx, f, cfg)
		loads = append(loads, res)
		features = append(features, engine.FileResult{Path: f.Path, Name: f.Name})
		printLoad(res)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			passed = false
			if ctx.Err() != nil {
				break
			}
			continue
		}
		if !res.Passed {
			passed = false
		}
	}
	var last *load.Result
	if len(loads) > 0 {
		last = &loads[len(loads)-1]
	}
	doc := report.Document{Mode: "load", Title: "gi load", Started: started, Duration: time.Since(started), Features: features, Load: last, Loads: loads}
	if err := emit(ctx, doc, *reportPath, *junitPath, *jsonPath, *cucumberPath, *otel); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if !passed {
		return 1
	}
	return 0
}

func cmdMock(args []string) int {
	fs := flag.NewFlagSet("mock", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	addr := fs.String("addr", ":18080", "")
	if err := fs.Parse(reorderFlags(args)); err != nil || fs.NArg() != 1 {
		usage(os.Stderr)
		return 2
	}
	f, err := feature.ParseFile(fs.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	srv := &http.Server{
		Addr:              *addr,
		Handler:           mock.Handler(f),
		ReadHeaderTimeout: 5 * time.Second,
	}
	fmt.Printf("mock %s on %s\n", f.Name, *addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func emit(ctx context.Context, doc report.Document, htmlPath, junitPath, jsonPath, cucumberPath, otel string) error {
	if htmlPath != "" {
		if err := report.WriteHTML(htmlPath, doc); err != nil {
			return err
		}
		fmt.Println("wrote", htmlPath)
	}
	if junitPath != "" {
		if err := report.WriteJUnit(junitPath, doc); err != nil {
			return err
		}
		fmt.Println("wrote", junitPath)
	}
	if jsonPath != "" {
		if err := report.WriteJSON(jsonPath, doc); err != nil {
			return err
		}
		fmt.Println("wrote", jsonPath)
	}
	if cucumberPath != "" {
		if err := report.WriteCucumber(cucumberPath, doc); err != nil {
			return err
		}
		fmt.Println("wrote", cucumberPath)
	}
	if otel != "" {
		if err := report.ExportOTLP(ctx, otel, doc); err != nil {
			fmt.Fprintln(os.Stderr, "otel:", err)
		}
	}
	return nil
}

func runFiles(ctx context.Context, paths []string, opt engine.Options, n int) ([]engine.FileResult, error) {
	parsed := make([]*feature.File, len(paths))
	for i, path := range paths {
		f, err := feature.ParseFile(path)
		if err != nil {
			return nil, err
		}
		parsed[i] = f
	}
	opt.Jobs = n
	return engine.RunFiles(ctx, parsed, opt, printFunctional)
}

func printFunctional(res engine.FileResult) {
	fmt.Printf("\n%s\n", res.Name)
	for _, sc := range res.Scenarios {
		switch {
		case sc.Skipped:
			fmt.Printf("  - %s (skipped)\n", sc.Name)
		case sc.Passed:
			fmt.Printf("  ✓ %s (%s)\n", sc.Name, sc.Duration.Round(time.Millisecond))
		default:
			fmt.Printf("  ✗ %s (%s)\n", sc.Name, sc.Duration.Round(time.Millisecond))
			if sc.Error != "" {
				fmt.Printf("%s\n", indent(sc.Error))
			}
		}
	}
}

func printLoad(res load.Result) {
	sec := res.Elapsed.Seconds()
	if sec <= 0 {
		sec = 1
	}
	fmt.Printf("  iterations: %d   failed: %d   missed: %d   late: %d\n", res.Iterations, res.FailedIterations, res.MissedStarts, res.LateStarts)
	if res.Open && res.Scheduled.P95 > 0 {
		fmt.Printf("  scheduled:  p95 %s\n", res.Scheduled.P95.Round(time.Microsecond))
	}
	if res.Goroutines > 0 {
		fmt.Printf("  runner:     cpu %s   rss %.1fMB   goroutines %d\n", res.CPU.Round(time.Millisecond), float64(res.RSS)/(1<<20), res.Goroutines)
	}
	fmt.Printf("  requests:   %d   transport errors: %d   rps: %.1f\n", res.Requests, res.TransportErrors, float64(res.Requests)/sec)
	fmt.Printf("  latency:    p50 %s   p95 %s   p99 %s   mean %s\n", res.Overall.P50.Round(time.Microsecond), res.Overall.P95.Round(time.Microsecond), res.Overall.P99.Round(time.Microsecond), res.Overall.Mean.Round(time.Microsecond))
	for _, ep := range res.Endpoints {
		fmt.Printf("  %-22s  n=%-6d p95=%s\n", ep.Name, ep.Count, ep.Stats.P95.Round(time.Microsecond))
	}
	for _, th := range res.Thresholds {
		mark := "pass"
		if !th.Passed {
			mark = "FAIL"
		}
		fmt.Printf("  threshold %s  actual %s  %s\n", th.Text, th.Actual, mark)
	}
	for _, sc := range res.Failures {
		fmt.Printf("  ✗ %s\n%s\n", sc.Name, indent(sc.Error))
	}
}

func indent(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = "    " + line
	}
	return strings.Join(lines, "\n")
}

// reorderFlags lets `gi test file.feature --var k=v` work. The standard flag
// parser stops at the first path, so flags are moved in front of file names.
func reorderFlags(args []string) []string {
	var flags []string
	var files []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			files = append(files, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			files = append(files, a)
			continue
		}
		flags = append(flags, a)
		name := strings.TrimLeft(a, "-")
		if strings.Contains(name, "=") {
			continue
		}
		if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
			i++
			flags = append(flags, args[i])
		}
	}
	return append(flags, files...)
}

func firstPath(files []string) string {
	if len(files) == 0 {
		return "."
	}
	return files[0]
}

func discover(args []string) ([]string, error) {
	if len(args) == 0 {
		for _, candidate := range []string{"features", "examples"} {
			if st, err := os.Stat(candidate); err == nil && st.IsDir() {
				args = []string{candidate}
				break
			}
		}
		if len(args) == 0 {
			args = []string{"."}
		}
	}
	var out []string
	for _, arg := range args {
		st, err := os.Stat(arg)
		if err != nil {
			return nil, err
		}
		if !st.IsDir() {
			out = append(out, arg)
			continue
		}
		err = filepath.WalkDir(arg, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				base := d.Name()
				if base == ".git" || base == "vendor" || strings.HasPrefix(base, ".") && path != arg {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.HasSuffix(path, ".feature") {
				out = append(out, path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	if len(out) == 0 {
		return nil, errors.New("no .feature files found")
	}
	return out, nil
}

type loadOptions struct {
	fs               *flag.FlagSet
	vus, vusTo       int
	rate, rateTo     float64
	stages           []string
	maxIn, rps       int
	duration, warmup time.Duration
}

func applyLoadFlags(cfg *load.Config, opt loadOptions) error {
	vusSet := flagGiven(opt.fs, "vus")
	vusToSet := flagGiven(opt.fs, "vus-to")
	rateSet := flagGiven(opt.fs, "rate")
	rateToSet := flagGiven(opt.fs, "rate-to")
	if (vusSet || vusToSet) && (rateSet || rateToSet) {
		return errors.New("load profile is either virtual users or a start rate")
	}
	if len(opt.stages) > 0 && (vusSet || vusToSet || rateSet || rateToSet || opt.duration > 0) {
		return errors.New("load stages replace virtual users, a start rate, and duration")
	}
	if (vusSet && opt.vus < 0) || (vusToSet && opt.vusTo < 0) || opt.rate < 0 || opt.rateTo < 0 || opt.rps < 0 || opt.warmup < 0 || opt.maxIn < 0 {
		return errors.New("load profile values must be finite and nonnegative")
	}
	if flagGiven(opt.fs, "max-inflight") {
		cfg.MaxInFlight = opt.maxIn
	}
	switch {
	case len(opt.stages) > 0:
		cfg.VUs, cfg.VUsTo, cfg.UsersToSet = 0, 0, false
		cfg.Rate, cfg.RateTo, cfg.RateSet, cfg.RateToSet = 0, 0, false, false
		cfg.Duration = 0
		cfg.Stages = nil
		cfg.StageText = append([]string{}, opt.stages...)
	case rateSet || rateToSet:
		cfg.StageText = nil
		cfg.Stages = nil
		cfg.VUs = 0
		cfg.VUsTo = 0
		cfg.UsersToSet = false
		if rateSet {
			cfg.Rate = opt.rate
			cfg.RateSet = true
		}
		if rateToSet {
			cfg.RateTo = opt.rateTo
			cfg.RateToSet = true
		}
	case vusSet || vusToSet:
		cfg.StageText = nil
		cfg.Stages = nil
		cfg.Rate = 0
		cfg.RateSet = false
		cfg.RateTo = 0
		cfg.RateToSet = false
		if vusSet {
			cfg.VUs = opt.vus
		}
		if vusToSet {
			cfg.VUsTo = opt.vusTo
			cfg.UsersToSet = true
			if !vusSet && cfg.VUs < 1 {
				cfg.VUs = 10
			}
		}
	}
	if opt.duration > 0 {
		cfg.Duration = opt.duration
	}
	if flagGiven(opt.fs, "warmup") {
		cfg.Warmup = opt.warmup
	}
	if flagGiven(opt.fs, "rps") {
		cfg.RPS = opt.rps
	}
	staged := len(cfg.StageText) > 0 || len(cfg.Stages) > 0
	if !staged && !(cfg.RateSet || cfg.RateToSet || cfg.Rate > 0) && !cfg.UsersToSet && cfg.VUs < 1 {
		cfg.VUs = 10
	}
	if !staged && cfg.Duration <= 0 {
		cfg.Duration = 10 * time.Second
	}
	return nil
}

func flagGiven(fs *flag.FlagSet, name string) bool {
	set := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			set = true
		}
	})
	return set
}

type multi []string

func (m *multi) String() string { return strings.Join(*m, ",") }
func (m *multi) Set(s string) error {
	*m = append(*m, s)
	return nil
}
func (m *multi) Map() (map[string]any, error) {
	out := map[string]any{}
	for _, item := range *m {
		k, v, ok := strings.Cut(item, "=")
		if !ok || k == "" {
			return nil, fmt.Errorf("--var expects key=value, got %q", item)
		}
		out[k] = v
	}
	return out, nil
}
