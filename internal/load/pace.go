package load

import (
	"context"
	"errors"
	"math"
	"strconv"
	"strings"
	"time"
)

func waitUntil(ctx context.Context, due time.Time) error {
	d := time.Until(due)
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// Normalize fills defaults and rejects a profile that mixes users with a start rate.
func Normalize(cfg Config) (Config, error) {
	if cfg.Warmup < 0 || cfg.VUs < 0 || cfg.VUsTo < 0 || cfg.RPS < 0 || cfg.MaxInFlight < 0 || badNum(cfg.Rate) || badNum(cfg.RateTo) {
		return Config{}, errors.New("load profile values must be finite and nonnegative")
	}
	for _, raw := range cfg.StageText {
		st, err := ParseStage(raw)
		if err != nil {
			return Config{}, err
		}
		cfg.Stages = append(cfg.Stages, st)
	}
	cfg.StageText = nil
	legacy := cfg.VUs > 0 || cfg.UsersToSet || cfg.RateSet || cfg.RateToSet || cfg.Rate > 0
	if len(cfg.Stages) > 0 && legacy {
		return Config{}, errors.New("load stages replace virtual users and a start rate")
	}
	if len(cfg.Stages) == 0 {
		if cfg.Duration <= 0 {
			cfg.Duration = 10 * time.Second
		}
		st, err := legacyStage(cfg)
		if err != nil {
			return Config{}, err
		}
		cfg.Stages = []Stage{st}
	}
	sum, err := stageSum(cfg.Stages)
	if err != nil {
		return Config{}, err
	}
	if cfg.Warmup > 0 && sum > time.Duration(math.MaxInt64)-cfg.Warmup {
		return Config{}, errors.New("stage duration overflows")
	}
	cfg.Duration = sum
	cfg.VUs, cfg.VUsTo, cfg.UsersToSet = 0, 0, false
	cfg.Rate, cfg.RateTo, cfg.RateSet, cfg.RateToSet = 0, 0, false, false
	if cfg.MaxInFlight > 0 && !cfg.Stages[0].Rate {
		return Config{}, errors.New("maxInflight applies to a start rate")
	}
	return cfg, nil
}

func legacyStage(cfg Config) (Stage, error) {
	if cfg.RateSet || cfg.RateToSet || cfg.Rate > 0 {
		to := cfg.Rate
		if cfg.RateToSet {
			to = cfg.RateTo
		}
		if cfg.Rate == 0 && to == 0 {
			return Stage{}, errors.New("start rate is zero")
		}
		if cfg.VUs > 0 || cfg.UsersToSet {
			return Stage{}, errors.New("load profile is either virtual users or a start rate")
		}
		return Stage{Rate: true, From: cfg.Rate, To: to, ToSet: cfg.RateToSet, Duration: cfg.Duration}, nil
	}
	if cfg.UsersToSet && cfg.VUs == 0 && cfg.VUsTo == 0 {
		return Stage{}, errors.New("virtual users ramp is zero")
	}
	from := cfg.VUs
	if !cfg.UsersToSet && from < 1 {
		from = 1
	}
	st := Stage{From: float64(from), Duration: cfg.Duration}
	if cfg.UsersToSet {
		st.To = float64(cfg.VUsTo)
		st.ToSet = true
	}
	return st, nil
}

func stageSum(stages []Stage) (time.Duration, error) {
	if len(stages) == 0 {
		return 0, errors.New("load profile has no stage")
	}
	var sum time.Duration
	rate := stages[0].Rate
	for _, st := range stages {
		if err := validateStage(st); err != nil {
			return 0, err
		}
		if st.Rate != rate {
			return 0, errors.New("load stages are either virtual users or a start rate")
		}
		if int64(sum) > math.MaxInt64-int64(st.Duration) {
			return 0, errors.New("stage duration overflows")
		}
		sum += st.Duration
	}
	return sum, nil
}

func badNum(v float64) bool {
	return v < 0 || math.IsNaN(v) || math.IsInf(v, 0)
}

func (cfg Config) openLoop() bool {
	if len(cfg.Stages) > 0 {
		return cfg.Stages[0].Rate
	}
	return cfg.RateSet || cfg.RateToSet || cfg.Rate > 0
}

// Describe is the profile line printed for a run.
func Describe(cfg Config) string {
	if len(cfg.Stages) == 0 {
		return describeStage(Stage{Rate: cfg.openLoop(), From: cfg.Rate, To: cfg.RateTo, ToSet: cfg.RateToSet})
	}
	parts := make([]string, len(cfg.Stages))
	for i, st := range cfg.Stages {
		parts[i] = describeStage(st)
	}
	return strings.Join(parts, " then ")
}

func num(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}
