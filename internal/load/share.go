package load

import (
	"errors"
	"fmt"
	"math"
	"slices"
)

// Share gives injector index one portion of a normalized profile.
// The parent keeps the thresholds and judges them after the portions merge.
func Share(cfg Config, index, n int) (Config, error) {
	if n <= 0 || index < 0 || index >= n {
		return Config{}, errors.New("injector share is out of range")
	}
	out := cfg
	out.Stages = slices.Clone(cfg.Stages)
	out.Injectors = 1
	if n == 1 {
		out.Thresholds = append([]string{}, cfg.Thresholds...)
		return out, nil
	}
	out.Thresholds = nil
	for i := range out.Stages {
		st := &out.Stages[i]
		st.From = splitLevel(st.From, st.Rate, index, n)
		if st.ToSet {
			st.To = splitLevel(st.To, st.Rate, index, n)
		}
		if err := validateStage(*st); err != nil {
			return Config{}, fmt.Errorf("injector %d of %d: %w", index+1, n, err)
		}
	}
	rps, err := splitCap(cfg.RPS, index, n, "the request cap")
	if err != nil {
		return Config{}, err
	}
	inflight, err := splitCap(cfg.MaxInFlight, index, n, "max-inflight")
	if err != nil {
		return Config{}, err
	}
	out.RPS = rps
	out.MaxInFlight = inflight
	return out, nil
}

func splitLevel(total float64, rate bool, index, n int) float64 {
	if rate {
		return splitRate(total, index, n)
	}
	return float64(splitCount(int(math.Round(total)), index, n))
}

func splitCount(total, index, n int) int {
	if total <= 0 || n <= 1 {
		return total
	}
	base := total / n
	if index < total%n {
		return base + 1
	}
	return base
}

func splitRate(total float64, index, n int) float64 {
	if total == 0 || n <= 1 {
		return total
	}
	units := int64(math.Round(total * 1e6))
	base := units / int64(n)
	if int64(index) < units%int64(n) {
		base++
	}
	return float64(base) / 1e6
}

func splitCap(total, index, n int, name string) (int, error) {
	if total == 0 {
		return 0, nil
	}
	got := splitCount(total, index, n)
	if got < 1 {
		return 0, fmt.Errorf("injector %d of %d has no share of %s", index+1, n, name)
	}
	return got, nil
}
