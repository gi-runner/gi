package load

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"sort"
	"time"

	"github.com/gi-runner/gi/internal/engine"
)

// Snapshot is one part's histograms and the full profile it belongs to.
// Merging snapshots keeps percentiles exact to the histogram resolution.
type Snapshot struct {
	Started          time.Time               `json:"started"`
	Elapsed          time.Duration           `json:"elapsed"`
	Profile          string                  `json:"profile,omitempty"`
	Duration         time.Duration           `json:"duration"`
	Thresholds       []string                `json:"thresholds,omitempty"`
	Part             Part                    `json:"part"`
	Open             bool                    `json:"open,omitempty"`
	VUs              int                     `json:"vus"`
	Iterations       int64                   `json:"iterations"`
	FailedIterations int64                   `json:"failedIterations"`
	Requests         int64                   `json:"requests"`
	TransportErrors  int64                   `json:"transportErrors"`
	EndpointLimit    int                     `json:"endpointLimit"`
	EndpointOverflow int64                   `json:"endpointOverflow"`
	MissedStarts     int64                   `json:"missedStarts"`
	LateStarts       int64                   `json:"lateStarts"`
	MaxStartLag      time.Duration           `json:"maxStartLag"`
	CPU              time.Duration           `json:"cpu"`
	RSS              uint64                  `json:"rss"`
	Goroutines       int                     `json:"goroutines"`
	Err              string                  `json:"err,omitempty"`
	Failures         []engine.ScenarioResult `json:"failures,omitempty"`
	Overall          wireHist                `json:"overall"`
	Scheduled        wireHist                `json:"scheduled"`
	Endpoints        []wireEndpoint          `json:"endpoints,omitempty"`
	Slots            [timelineSlots]wireSlot `json:"slots"`
}

type wireEndpoint struct {
	Name   string   `json:"name"`
	Errors int64    `json:"errors"`
	Lat    wireHist `json:"lat"`
}

type wireSlot struct {
	Lat    wireHist `json:"lat"`
	Errors int64    `json:"errors"`
	Users  int      `json:"users"`
}

type wireHist struct {
	N      int64    `json:"n"`
	Min    int64    `json:"min"`
	Max    int64    `json:"max"`
	Sum    float64  `json:"sum"`
	Bucket []uint64 `json:"bucket,omitempty"`
}

func (s *session) snapshot(res Result) Snapshot {
	locals := make([]*collector, len(s.collectors))
	for i := range s.collectors {
		locals[i] = &s.collectors[i]
	}
	got := fold(locals)
	all, sched, by, errN, slots, failures := got.all, got.sched, got.by, got.errN, got.slots, got.failures
	snap := Snapshot{
		Started:          res.Started,
		Elapsed:          res.Elapsed,
		Profile:          Describe(s.full),
		Duration:         s.full.Duration,
		Thresholds:       s.full.Thresholds,
		Part:             s.full.Part,
		Open:             res.Open,
		VUs:              res.VUs,
		Iterations:       res.Iterations,
		FailedIterations: res.FailedIterations,
		Requests:         res.Requests,
		TransportErrors:  res.TransportErrors,
		EndpointLimit:    res.EndpointLimit,
		EndpointOverflow: res.EndpointOverflow,
		MissedStarts:     res.MissedStarts,
		LateStarts:       res.LateStarts,
		MaxStartLag:      res.MaxStartLag,
		CPU:              res.CPU,
		RSS:              res.RSS,
		Goroutines:       res.Goroutines,
		Failures:         failures,
		Overall:          exportHist(all),
		Scheduled:        exportHist(sched),
	}
	names := make([]string, 0, len(by))
	for name := range by {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		snap.Endpoints = append(snap.Endpoints, wireEndpoint{Name: name, Errors: errN[name], Lat: exportHist(*by[name])})
	}
	for i := range slots {
		snap.Slots[i] = wireSlot{Lat: exportHist(slots[i].lat), Errors: slots[i].errN, Users: usersOf(s.userPeak[i].Load())}
	}
	return snap
}

func exportHist(h hist) wireHist {
	w := wireHist{N: h.n, Min: h.min, Max: h.max, Sum: h.sum}
	if h.n == 0 {
		return w
	}
	w.Bucket = append([]uint64(nil), h.bucket[:]...)
	return w
}

func (w wireHist) hist() hist {
	h := hist{n: w.N, min: w.Min, max: w.Max, sum: w.Sum}
	copy(h.bucket[:], w.Bucket)
	return h
}

func WriteSnapshot(path string, snap Snapshot) error {
	raw, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return os.WriteFile(path, raw, 0o600)
}

func ReadSnapshot(path string) (Snapshot, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Snapshot{}, err
	}
	var snap Snapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		return Snapshot{}, err
	}
	return snap, nil
}

// Merge joins every part of one profile and judges it with the recorded
// thresholds plus extra.
func Merge(snaps []Snapshot, extra []string) (Result, error) {
	if err := complete(snaps); err != nil {
		return Result{}, err
	}
	first := snaps[0]
	ths, err := ParseThresholds(append(slices.Clone(first.Thresholds), extra...))
	if err != nil {
		return Result{}, err
	}
	combined := combine(snaps)
	combined.Profile = first.Profile
	if first.Part.Of > 1 {
		combined.Profile += fmt.Sprintf(" across %d parts", first.Part.Of)
	}
	combined.EndpointLimit = first.EndpointLimit
	res := combined.result()
	applyVerdict(&res, ths, first.Duration)
	var runErr error
	for _, snap := range snaps {
		if snap.Err != "" {
			runErr = errors.New(snap.Err)
			res.Passed = false
			break
		}
	}
	return res, runErr
}

func complete(snaps []Snapshot) error {
	if len(snaps) == 0 {
		return errors.New("merge needs at least one snapshot")
	}
	first := snaps[0]
	if len(snaps) != first.Part.Of {
		return fmt.Errorf("merge has %d snapshots for a profile in %d parts", len(snaps), first.Part.Of)
	}
	seen := make([]bool, first.Part.Of)
	for _, snap := range snaps {
		if snap.Profile != first.Profile || snap.Part.Of != first.Part.Of || snap.Duration != first.Duration {
			return errors.New("snapshots come from different load profiles")
		}
		if snap.Part.Index < 0 || snap.Part.Index >= len(seen) || seen[snap.Part.Index] {
			return fmt.Errorf("part %d appears twice or is out of range", snap.Part.Index+1)
		}
		seen[snap.Part.Index] = true
	}
	return nil
}

func combine(snaps []Snapshot) Snapshot {
	var out Snapshot
	var all, sched hist
	by := map[string]*hist{}
	errN := map[string]int64{}
	var slots [timelineSlots]sampleSlot
	var users [timelineSlots]int
	for _, snap := range snaps {
		if out.Started.IsZero() || (!snap.Started.IsZero() && snap.Started.Before(out.Started)) {
			out.Started = snap.Started
		}
		if snap.Elapsed > out.Elapsed {
			out.Elapsed = snap.Elapsed
		}
		out.VUs += snap.VUs
		out.Iterations += snap.Iterations
		out.FailedIterations += snap.FailedIterations
		out.Requests += snap.Requests
		out.TransportErrors += snap.TransportErrors
		out.EndpointOverflow += snap.EndpointOverflow
		out.MissedStarts += snap.MissedStarts
		out.LateStarts += snap.LateStarts
		if snap.MaxStartLag > out.MaxStartLag {
			out.MaxStartLag = snap.MaxStartLag
		}
		out.CPU += snap.CPU
		if snap.RSS > out.RSS {
			out.RSS = snap.RSS
		}
		out.Goroutines += snap.Goroutines
		out.Open = out.Open || snap.Open
		all.merge(snap.Overall.hist())
		sched.merge(snap.Scheduled.hist())
		for _, ep := range snap.Endpoints {
			h := by[ep.Name]
			if h == nil {
				h = &hist{}
				by[ep.Name] = h
			}
			h.merge(ep.Lat.hist())
			errN[ep.Name] += ep.Errors
		}
		for i := range snap.Slots {
			slots[i].lat.merge(snap.Slots[i].Lat.hist())
			slots[i].errN += snap.Slots[i].Errors
			users[i] += snap.Slots[i].Users
		}
		out.Failures = append(out.Failures, snap.Failures...)
	}
	out.Overall = exportHist(all)
	out.Scheduled = exportHist(sched)
	names := make([]string, 0, len(by))
	for name := range by {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		out.Endpoints = append(out.Endpoints, wireEndpoint{Name: name, Errors: errN[name], Lat: exportHist(*by[name])})
	}
	for i := range slots {
		out.Slots[i] = wireSlot{Lat: exportHist(slots[i].lat), Errors: slots[i].errN, Users: users[i]}
	}
	return out
}

func (snap Snapshot) result() Result {
	sec := snap.Elapsed.Seconds()
	if sec <= 0 {
		sec = 1
	}
	endpoints := make([]Endpoint, 0, len(snap.Endpoints))
	for _, ep := range snap.Endpoints {
		h := ep.Lat.hist()
		endpoints = append(endpoints, Endpoint{
			Name: ep.Name, Count: h.n, Errors: ep.Errors, Stats: h.stats(), RPS: float64(h.n) / sec,
		})
	}
	failures := snap.Failures
	if len(failures) > 20 {
		failures = failures[:20]
	}
	overall := snap.Overall.hist()
	scheduled := snap.Scheduled.hist()
	res := Result{
		Started:          snap.Started,
		Elapsed:          snap.Elapsed,
		Profile:          snap.Profile,
		Open:             snap.Open,
		VUs:              snap.VUs,
		Iterations:       snap.Iterations,
		FailedIterations: snap.FailedIterations,
		Requests:         snap.Requests,
		TransportErrors:  snap.TransportErrors,
		EndpointLimit:    snap.EndpointLimit,
		EndpointOverflow: snap.EndpointOverflow,
		Endpoints:        endpoints,
		Failures:         failures,
		Overall:          overall.stats(),
		Scheduled:        scheduled.stats(),
		MissedStarts:     snap.MissedStarts,
		LateStarts:       snap.LateStarts,
		MaxStartLag:      snap.MaxStartLag,
		CPU:              snap.CPU,
		RSS:              snap.RSS,
		Goroutines:       snap.Goroutines,
	}
	if timeline := slotsOf(snap.Slots, snap.Elapsed); len(timeline) > 0 {
		res.Timeline = timeline
	}
	return res
}

func slotsOf(slots [timelineSlots]wireSlot, elapsed time.Duration) []Slot {
	saw := false
	for _, slot := range slots {
		if slot.Lat.N > 0 || slot.Users > 0 {
			saw = true
			break
		}
	}
	if !saw || elapsed <= 0 {
		return nil
	}
	width := elapsed / timelineSlots
	out := make([]Slot, timelineSlots)
	for i, slot := range slots {
		lat := slot.Lat.hist()
		stats := lat.stats()
		out[i] = Slot{
			Offset:   time.Duration(i) * width,
			Requests: slot.Lat.N,
			Errors:   slot.Errors,
			Mean:     stats.Mean,
			P95:      stats.P95,
			Users:    slot.Users,
		}
	}
	return out
}
