package load

import (
	"errors"
	"fmt"
	"math"
	"math/bits"
	"strconv"
	"strings"
	"time"
)

// A start later than this was not on the schedule. Timer jitter stays under it.
const lateAfter = 20 * time.Millisecond

// Stage is one segment of a load plan. Users and rate segments cannot mix.
// A spike jumps to From for Duration, then returns to the level before it.
type Stage struct {
	Rate     bool
	Spike    bool
	From     float64
	To       float64
	ToSet    bool
	Duration time.Duration
}

// ParseStage reads "users=1:40 duration=30s" or "spike rate=200 duration=10s".
func ParseStage(raw string) (Stage, error) {
	fields := strings.Fields(strings.TrimSpace(raw))
	if len(fields) == 0 {
		return Stage{}, fmt.Errorf("stage %q is empty", raw)
	}
	var st Stage
	if strings.EqualFold(fields[0], "spike") {
		st.Spike = true
		fields = fields[1:]
	}
	var sawLevel, sawDuration bool
	for _, field := range fields {
		k, v, ok := strings.Cut(field, "=")
		if !ok {
			return Stage{}, fmt.Errorf("stage %q", raw)
		}
		switch strings.ToLower(k) {
		case "duration":
			d, err := time.ParseDuration(v)
			if err != nil || d <= 0 {
				return Stage{}, fmt.Errorf("stage duration %q", v)
			}
			st.Duration = d
			sawDuration = true
		case "users", "rate":
			if sawLevel {
				return Stage{}, fmt.Errorf("stage %q sets users and rate", raw)
			}
			from, to, toSet, err := parseSpan(v)
			if err != nil || from < 0 || to < 0 || math.IsNaN(from) || math.IsNaN(to) {
				return Stage{}, fmt.Errorf("stage level %q", v)
			}
			st.Rate = strings.EqualFold(k, "rate")
			if !st.Rate && (from != math.Trunc(from) || to != math.Trunc(to)) {
				return Stage{}, fmt.Errorf("stage users %q", v)
			}
			st.From, st.To, st.ToSet = from, to, toSet
			sawLevel = true
		default:
			return Stage{}, fmt.Errorf("stage %q has unknown field %s", raw, k)
		}
	}
	if !sawLevel || !sawDuration {
		return Stage{}, fmt.Errorf("stage %q needs a level and a duration", raw)
	}
	if st.Spike && st.ToSet {
		return Stage{}, fmt.Errorf("stage %q spike is a single level", raw)
	}
	if err := validateStage(st); err != nil {
		return Stage{}, fmt.Errorf("stage %q: %w", raw, err)
	}
	return st, nil
}

// validateStage is the check for parsed text and for a Stage set on Config.
func validateStage(st Stage) error {
	if st.Duration <= 0 {
		return errors.New("stage duration must be positive")
	}
	if badNum(st.From) || (st.ToSet && badNum(st.To)) {
		return errors.New("stage level must be finite and nonnegative")
	}
	if st.Spike && st.ToSet {
		return errors.New("a spike is a single level")
	}
	if !st.Rate {
		limit := math.Ldexp(1, strconv.IntSize-1)
		if st.From >= limit || (st.ToSet && st.To >= limit) {
			return errors.New("stage users exceed the integer range")
		}
	}
	if !st.Rate && (st.From != math.Trunc(st.From) || (st.ToSet && st.To != math.Trunc(st.To))) {
		return errors.New("stage users must be whole numbers")
	}
	if st.Rate && st.From == 0 && (!st.ToSet || st.To == 0) {
		return errors.New("start rate is zero")
	}
	if !st.Rate && !st.Spike && st.From == 0 && (!st.ToSet || st.To == 0) {
		return errors.New("virtual users are zero")
	}
	return nil
}

func parseSpan(v string) (from, to float64, toSet bool, err error) {
	if a, b, ok := strings.Cut(v, ":"); ok {
		from, err = strconv.ParseFloat(a, 64)
		if err != nil {
			return 0, 0, false, err
		}
		to, err = strconv.ParseFloat(b, 64)
		return from, to, true, err
	}
	from, err = strconv.ParseFloat(v, 64)
	return from, from, false, err
}

type plan struct {
	warmup time.Duration
	stages []Stage
	spans  []span
	dur    time.Duration
}

func newPlan(warmup time.Duration, stages []Stage) plan {
	p := plan{warmup: warmup, stages: stages}
	p.dur = warmup
	for _, st := range stages {
		p.dur += st.Duration
	}
	p.spans = p.buildSpans()
	return p
}

func (p plan) window() time.Duration { return p.dur }

func (p plan) rate() bool {
	return len(p.stages) > 0 && p.stages[0].Rate
}

func (p plan) peakRate() float64 {
	var peak float64
	for _, st := range p.stages {
		peak = math.Max(peak, math.Max(st.From, st.To))
	}
	return peak
}

type level struct {
	At time.Duration
	N  int
}

// levels is the closed-loop population. t=0 holds the opening level through warmup.
// A spike replaces that level until the segment ends, then restores it.
func (p plan) levels() []level {
	levelN := 0
	if len(p.stages) > 0 && !p.stages[0].Spike {
		levelN = int(p.stages[0].From)
	}
	out := []level{{N: levelN}}
	t := p.warmup
	for _, st := range p.stages {
		if st.Spike {
			out = append(out, level{At: t, N: int(st.From)})
			out = append(out, level{At: t + st.Duration, N: levelN})
			t += st.Duration
			continue
		}
		from := int(st.From)
		to := from
		if st.ToSet {
			to = int(st.To)
		}
		if from != levelN {
			out = append(out, level{At: t, N: from})
			levelN = from
		}
		if to != from {
			steps := to - from
			dir := 1
			if steps < 0 {
				dir = -1
				steps = -steps
			}
			for k := 1; k <= steps; k++ {
				hi, lo := bits.Mul64(uint64(st.Duration), uint64(k)) //nolint:gosec // Validated positive duration and 1 <= k <= steps.
				offset, _ := bits.Div64(hi, lo, uint64(steps))       //nolint:gosec // A ramp has a positive step count.
				at := t + time.Duration(offset)                      //nolint:gosec // The quotient is bounded by st.Duration.
				out = append(out, level{At: at, N: from + dir*k})
			}
			levelN = to
		}
		t += st.Duration
	}
	return out
}

func (p plan) maxUsers() int {
	m := 1
	for _, st := range p.stages {
		m = max(m, int(st.From))
		if st.ToSet {
			m = max(m, int(st.To))
		}
	}
	return m
}

type span struct {
	start    time.Duration
	dur      time.Duration
	from, to float64
	base     float64
}

func (s span) area() float64 {
	sec := s.dur.Seconds()
	if sec <= 0 {
		return 0
	}
	return s.from*sec + (s.to-s.from)*sec/2
}

func (s span) invert(n float64) (time.Duration, bool) {
	if n == 0 {
		return 0, true
	}
	if s.dur <= 0 || n < 0 {
		return 0, false
	}
	T := s.dur.Seconds()
	if math.Abs(s.to-s.from) < 1e-12 {
		if s.from <= 0 {
			return 0, false
		}
		return time.Duration(n / s.from * float64(time.Second)), true
	}
	a := (s.to - s.from) / (2 * T)
	b := s.from
	disc := b*b + 4*a*n
	if disc < 0 {
		return 0, false
	}
	sqrt := math.Sqrt(disc)
	// Rationalizing the positive root avoids cancellation for nearly flat ramps.
	t := 2 * n / (b + sqrt)
	if t < 0 || t > T {
		t = (-b - sqrt) / (2 * a)
	}
	if t < 0 || math.IsNaN(t) || math.IsInf(t, 0) {
		return 0, false
	}
	return time.Duration(t * float64(time.Second)), true
}

// buildSpans is the open-loop rate. Warmup holds the opening rate.
func (p plan) buildSpans() []span {
	open := 0.0
	if len(p.stages) > 0 && !p.stages[0].Spike {
		open = p.stages[0].From
	}
	var out []span
	base := 0.0
	t := time.Duration(0)
	if p.warmup > 0 {
		out = append(out, span{dur: p.warmup, from: open, to: open})
		base = out[0].area()
		t = p.warmup
	}
	for _, st := range p.stages {
		from, to := st.From, st.From
		if !st.Spike && st.ToSet {
			to = st.To
		}
		sp := span{start: t, dur: st.Duration, from: from, to: to, base: base}
		out = append(out, sp)
		base += sp.area()
		t += st.Duration
	}
	return out
}

// due is arrival n. The integral of the piecewise rate reaches n at that time.
// A plan that opens at zero starts at integral 1, so nothing is injected at t=0.
func (p plan) due(n int) (time.Duration, bool) {
	if n < 0 || len(p.spans) == 0 {
		return 0, false
	}
	target := float64(n)
	if p.spans[0].from == 0 {
		target = float64(n + 1)
	}
	for _, sp := range p.spans {
		if target > sp.base+sp.area() {
			continue
		}
		x, ok := sp.invert(target - sp.base)
		if !ok {
			return 0, false
		}
		due := sp.start + x
		if due < 0 || due >= p.dur {
			return 0, false
		}
		return due, true
	}
	return 0, false
}

func describeStage(st Stage) string {
	if st.Spike {
		if st.Rate {
			return "spike rate " + num(st.From) + "/s"
		}
		return fmt.Sprintf("spike users %d", int(st.From))
	}
	if st.Rate {
		if st.ToSet && st.To != st.From {
			return fmt.Sprintf("ramp rate %s..%s/s", num(st.From), num(st.To))
		}
		return fmt.Sprintf("constant rate %s/s", num(st.From))
	}
	if st.ToSet && st.To != st.From {
		return fmt.Sprintf("ramp users %d..%d", int(st.From), int(st.To))
	}
	return fmt.Sprintf("constant users %d", max(int(st.From), 1))
}
