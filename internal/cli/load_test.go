package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/spf13/pflag"

	"github.com/gi-runner/gi/internal/load"
)

func TestMetricStream(t *testing.T) {
	var buf bytes.Buffer
	calls := 0
	tick := fanout(nil, metricStream(&buf, "shop"), func(load.Progress) { calls++ })
	tick(load.Progress{Elapsed: time.Second, Users: 3, Requests: 40, Failed: 1})
	tick(load.Progress{Elapsed: 2 * time.Second, Users: 4, Requests: 100, Failed: 1})
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 || calls != 2 {
		t.Fatalf("%d calls: %q", calls, buf.String())
	}
	var last metricLine
	if err := json.Unmarshal([]byte(lines[1]), &last); err != nil {
		t.Fatal(err)
	}
	if last.Feature != "shop" || last.Elapsed != 2 || last.Users != 4 || last.Requests != 100 || last.RPS != 60 {
		t.Fatalf("%+v", last)
	}
	if fanout(nil, nil) != nil {
		t.Fatal("an empty fanout ticks")
	}
}

func TestApplyLoadFlags(t *testing.T) {
	cfg, err := loadFlags(t, "--vus-to", "40", "--duration", "5s")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.VUs != 10 || !cfg.UsersToSet || cfg.VUsTo != 40 || cfg.Duration != 5*time.Second {
		t.Fatalf("%+v", cfg)
	}
	cfg, err = loadFlags(t, "--rate", "12.5")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.VUs != 0 || cfg.Rate != 12.5 || load.Describe(cfg) != "constant rate 12.5/s" {
		t.Fatalf("%+v", cfg)
	}
	if _, err := loadFlags(t, "--vus", "4", "--rate", "10"); err == nil {
		t.Fatal("accepted users and a start rate")
	}
	cfg, err = loadFlags(t, "--rate", "0")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := load.Normalize(cfg); err == nil {
		t.Fatal("accepted a zero start rate")
	}
}

func loadFlags(t *testing.T, args ...string) (load.Config, error) {
	t.Helper()
	fs := pflag.NewFlagSet("load", pflag.ContinueOnError)
	vus := fs.Int("vus", 0, "")
	vusTo := fs.Int("vus-to", 0, "")
	rate := fs.Float64("rate", 0, "")
	rateTo := fs.Float64("rate-to", 0, "")
	duration := fs.Duration("duration", 0, "")
	warmup := fs.Duration("warmup", 0, "")
	rps := fs.Int("rps", 0, "")
	if err := fs.Parse(args); err != nil {
		t.Fatal(err)
	}
	cfg := load.Config{}
	err := applyLoadFlags(&cfg, loadOptions{
		fs: fs, vus: *vus, vusTo: *vusTo, rate: *rate, rateTo: *rateTo,
		duration: *duration, warmup: *warmup, rps: *rps,
	})
	return cfg, err
}

func TestRateFlagOverridesFeatureStages(t *testing.T) {
	fs := pflag.NewFlagSet("load", pflag.ContinueOnError)
	rate := fs.Float64("rate", 0, "")
	fs.Int("vus", 0, "")
	fs.Int("vus-to", 0, "")
	fs.Float64("rate-to", 0, "")
	fs.Duration("duration", 0, "")
	fs.Duration("warmup", 0, "")
	fs.Int("rps", 0, "")
	if err := fs.Parse([]string{"--rate", "10"}); err != nil {
		t.Fatal(err)
	}
	cfg := load.Config{StageText: []string{"users=1:5 duration=1s"}}
	if err := applyLoadFlags(&cfg, loadOptions{fs: fs, rate: *rate}); err != nil {
		t.Fatal(err)
	}
	if len(cfg.StageText) != 0 || len(cfg.Stages) != 0 {
		t.Fatalf("feature stages survived an override: %+v", cfg)
	}
	norm, err := load.Normalize(cfg)
	if err != nil || load.Describe(norm) != "constant rate 10/s" {
		t.Fatalf("%+v %v", norm, err)
	}

	plain := pflag.NewFlagSet("load", pflag.ContinueOnError)
	plain.Int("vus", 0, "")
	plain.Int("vus-to", 0, "")
	plain.Float64("rate", 0, "")
	plain.Float64("rate-to", 0, "")
	plain.Duration("duration", 0, "")
	plain.Duration("warmup", 0, "")
	plain.Int("rps", 0, "")
	if err := plain.Parse(nil); err != nil {
		t.Fatal(err)
	}
	staged := load.Config{StageText: []string{"users=1:5 duration=1s"}}
	if err := applyLoadFlags(&staged, loadOptions{fs: plain}); err != nil {
		t.Fatal(err)
	}
	norm, err = load.Normalize(staged)
	if err != nil || load.Describe(norm) != "ramp users 1..5" {
		t.Fatalf("%+v %v", norm, err)
	}
}

func TestEndpointLimitFlag(t *testing.T) {
	for _, arg := range []string{"3", "0", "-1"} {
		fs := pflag.NewFlagSet("load", pflag.ContinueOnError)
		limit := fs.Int("max-endpoints", 0, "")
		if err := fs.Parse([]string{"--max-endpoints", arg}); err != nil {
			t.Fatal(err)
		}
		cfg := load.Config{}
		err := applyLoadFlags(&cfg, loadOptions{fs: fs, maxEndpoints: *limit})
		if arg != "3" {
			if err == nil {
				t.Fatalf("accepted limit %s", arg)
			}
			continue
		}
		if err != nil || cfg.MaxEndpoints != 3 {
			t.Fatalf("limit = %d: %v", cfg.MaxEndpoints, err)
		}
	}
}
