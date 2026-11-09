package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gi-runner/gi/internal/engine"
	"github.com/gi-runner/gi/internal/load"
	"github.com/gi-runner/gi/internal/report"
)

// parsePart reads a 1-based "i/n" share.
func parsePart(raw string) (load.Part, error) {
	indexText, countText, ok := strings.Cut(raw, "/")
	index, errIndex := strconv.Atoi(indexText)
	n, errCount := strconv.Atoi(countText)
	if !ok || errIndex != nil || errCount != nil || n < 1 || index < 1 || index > n {
		return load.Part{}, fmt.Errorf("--part expects i/n with 1 <= i <= n, got %q", raw)
	}
	return load.Part{Index: index - 1, Of: n}, nil
}

func waitStart(ctx context.Context, raw string) error {
	if raw == "" {
		return nil
	}
	at, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return fmt.Errorf("--start-at expects RFC 3339 time: %w", err)
	}
	timer := time.NewTimer(time.Until(at))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// liveProgress redraws one status line on a terminal and stays silent otherwise.
func liveProgress(w *os.File, label string) func(load.Progress) {
	if st, err := w.Stat(); err != nil || st.Mode()&os.ModeCharDevice == 0 {
		return nil
	}
	var last int64
	if label != "" {
		label += "  "
	}
	return func(p load.Progress) {
		_, _ = fmt.Fprintf(w, "\r  %s%s / %s  users %d  requests %d  rps %d  failed %d\033[K",
			label, p.Elapsed.Truncate(time.Second), p.Total.Truncate(time.Second), p.Users, p.Requests, p.Requests-last, p.Failed)
		last = p.Requests
	}
}

type metricLine struct {
	Time     time.Time `json:"time"`
	Feature  string    `json:"feature"`
	Elapsed  float64   `json:"elapsed"`
	Users    int       `json:"users"`
	Requests int64     `json:"requests"`
	Failed   int64     `json:"failed"`
	RPS      int64     `json:"rps"`
}

// metricStream writes one JSON line per progress tick; requests and failed are cumulative.
func metricStream(w io.Writer, name string) func(load.Progress) {
	enc := json.NewEncoder(w)
	var last int64
	return func(p load.Progress) {
		_ = enc.Encode(metricLine{
			Time: time.Now().UTC(), Feature: name, Elapsed: p.Elapsed.Seconds(),
			Users: p.Users, Requests: p.Requests, Failed: p.Failed, RPS: p.Requests - last,
		})
		last = p.Requests
	}
}

type scrape struct {
	mu      sync.Mutex
	order   []string
	samples map[string]scrapeSample
}

type scrapeSample struct {
	p    load.Progress
	rps  int64
	last int64
}

func (s *scrape) record(name string, p load.Progress) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.samples == nil {
		s.samples = map[string]scrapeSample{}
	}
	if _, ok := s.samples[name]; !ok {
		s.order = append(s.order, name)
	}
	cur := s.samples[name]
	cur.rps = p.Requests - cur.last
	cur.last = p.Requests
	cur.p = p
	s.samples[name] = cur
}

func (s *scrape) serve(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	order := append([]string(nil), s.order...)
	samples := maps.Clone(s.samples)
	s.mu.Unlock()
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	write := func(help, kind, metric string, value func(scrapeSample) int64) {
		_, _ = fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s %s\n", metric, help, metric, kind)
		for _, name := range order {
			_, _ = fmt.Fprintf(w, "%s{feature=%q} %d\n", metric, name, value(samples[name]))
		}
	}
	write("Virtual users in flight.", "gauge", "gi_vus", func(s scrapeSample) int64 { return int64(s.p.Users) })
	write("HTTP requests completed.", "counter", "gi_http_reqs", func(s scrapeSample) int64 { return s.p.Requests })
	write("Failed iterations.", "counter", "gi_http_req_failed", func(s scrapeSample) int64 { return s.p.Failed })
	write("Requests in the last second.", "gauge", "gi_http_req_rate", func(s scrapeSample) int64 { return s.rps })
}

func listenMetrics(ctx context.Context, addr string, s *scrape) (stop func(), bound string, err error) {
	if addr == "" {
		return func() {}, "", nil
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", s.serve)
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 2 * time.Second}
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", addr)
	if err != nil {
		return nil, "", err
	}
	done := make(chan struct{})
	go func() {
		_ = srv.Serve(ln)
		close(done)
	}()
	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()
	return func() {
		_ = srv.Close()
		<-done
	}, ln.Addr().String(), nil
}

// progressBoard redraws one status line per profile on a terminal.
type progressBoard struct {
	mu    sync.Mutex
	w     *os.File
	on    bool
	order []string
	line  map[string]string
	drawn int
}

func newProgressBoard(w *os.File) *progressBoard {
	b := &progressBoard{w: w, line: map[string]string{}}
	if st, err := w.Stat(); err == nil && st.Mode()&os.ModeCharDevice != 0 {
		b.on = true
	}
	return b
}

func (b *progressBoard) set(label, text string) {
	if b == nil || !b.on {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.line[label]; !ok {
		b.order = append(b.order, label)
	}
	b.line[label] = text
	if b.drawn > 0 {
		_, _ = fmt.Fprintf(b.w, "\033[%dA", b.drawn)
	}
	for _, name := range b.order {
		_, _ = fmt.Fprintf(b.w, "\r  %s  %s\033[K\n", name, b.line[name])
	}
	b.drawn = len(b.order)
}

func (b *progressBoard) clear() {
	if b == nil || !b.on || b.drawn == 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	_, _ = fmt.Fprintf(b.w, "\033[%dA", b.drawn)
	for range b.drawn {
		_, _ = fmt.Fprint(b.w, "\r\033[K\n")
	}
	_, _ = fmt.Fprintf(b.w, "\033[%dA", b.drawn)
	b.drawn = 0
}

type lockWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lockWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

func fanout(fns ...func(load.Progress)) func(load.Progress) {
	fns = slices.DeleteFunc(fns, func(fn func(load.Progress)) bool { return fn == nil })
	if len(fns) == 0 {
		return nil
	}
	return func(p load.Progress) {
		for _, fn := range fns {
			fn(p)
		}
	}
}

func clearProgress(w *os.File, progress func(load.Progress)) {
	if progress != nil {
		_, _ = fmt.Fprint(w, "\r\033[K")
	}
}

func runMerge(paths []string, opt loadCommandOptions) int {
	snaps := make([]load.Snapshot, len(paths))
	for i, path := range paths {
		snap, err := load.ReadSnapshot(path)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		snaps[i] = snap
	}
	res, err := load.Merge(snaps, opt.thresholds)
	if err != nil && res.Requests == 0 {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	fmt.Printf("load merge  %s  duration=%s\n", res.Profile, snaps[0].Duration)
	printLoad(res)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	doc := report.Document{Mode: "load", Title: "gi load", Started: res.Started, Duration: res.Elapsed, Features: []engine.FileResult{{Name: "merge"}}, Load: &res, Loads: []load.Result{res}}
	if err := emit(ctx, doc, opt.reportPath, opt.junitPath, opt.jsonPath, opt.cucumberPath, opt.otel); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if !res.Passed {
		return 1
	}
	return 0
}
