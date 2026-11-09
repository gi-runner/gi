package gi

import (
	"context"

	"github.com/gi-runner/gi/internal/adapters"
	"github.com/gi-runner/gi/internal/feature"
	"github.com/gi-runner/gi/internal/load"
	"github.com/gi-runner/gi/internal/mock"
	"github.com/gi-runner/gi/internal/suite"
)

// LoadOptions configures a load run. Jobs and Output do not apply.
type LoadOptions struct {
	Options
	// Stages replace the feature's profile, one --stage value each,
	// such as "users=1:50 duration=30s" or "rate=100 duration=1m".
	Stages []string
	// Thresholds are added to the feature's, such as "p95 < 300ms".
	Thresholds []string
}

// LoadResult holds latency, throughput, thresholds, and failures of a load run.
type LoadResult = load.Result

// Load runs one feature file under its gi-load profile or opt.Stages.
// A run that misses a threshold returns a result with Passed false and a nil error.
func Load(ctx context.Context, path string, opt LoadOptions) (LoadResult, error) {
	steps, err := opt.compile()
	if err != nil {
		return LoadResult{}, err
	}
	f, err := feature.ParseFile(path)
	if err != nil {
		return LoadResult{}, err
	}
	cfg := load.Config{
		LoadSpec: f.Load.Clone(),
		Vars:     opt.Vars, Tag: opt.Tags, Timeout: opt.Timeout, Env: opt.Env, Client: opt.Client,
		Suite: suite.Open(path, opt.Env), Dial: adapters.Dial, NewPage: adapters.NewPage, StartMock: mock.Start,
		Steps: steps, Helpers: opt.Helpers,
	}
	if len(opt.Stages) > 0 {
		cfg.LoadSpec = cfg.WithStages(opt.Stages)
	}
	cfg.Thresholds = append(cfg.Thresholds, opt.Thresholds...)
	return load.Run(ctx, f, cfg)
}
