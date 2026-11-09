package load

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/gi-runner/gi/internal/engine"
	"github.com/gi-runner/gi/internal/feature"
)

// mix runs one scenario per iteration in proportion to its @weight. Smooth
// weighted round robin keeps every run of total-weight iterations exact.
type mix struct {
	mu     sync.Mutex
	files  []*feature.File
	weight []int
	cur    []int
	total  int
}

// newMix returns nil when no scenario carries @weight; each iteration then runs the file.
// A selected scenario without @weight counts as weight 1.
func newMix(f *feature.File, opt engine.Options) (*mix, error) {
	weighted := false
	var setup []feature.Scenario
	for _, sc := range f.Scenarios {
		if sc.IsSetup {
			setup = append(setup, sc)
		}
		if _, ok, err := weightOf(sc.Tags); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", f.Path, sc.Line, err)
		} else if ok {
			weighted = true
		}
	}
	if !weighted {
		return nil, nil //nolint:nilnil // no mix means the whole file is one iteration
	}
	m := &mix{}
	for i := 0; i < len(f.Scenarios); {
		sc := f.Scenarios[i]
		j := i + 1
		for sc.OutlineID != 0 && j < len(f.Scenarios) && f.Scenarios[j].OutlineID == sc.OutlineID {
			j++
		}
		group := f.Scenarios[i:j]
		i = j
		ok, err := engine.Selected(sc, opt)
		if err != nil {
			return nil, err
		}
		w, set, _ := weightOf(sc.Tags)
		if !set {
			w = 1
		}
		if !ok || w == 0 {
			continue
		}
		file := *f
		file.Scenarios = append(append([]feature.Scenario{}, setup...), group...)
		m.files = append(m.files, &file)
		m.weight = append(m.weight, w)
		m.total += w
	}
	if m.total == 0 {
		return nil, errors.New("no weighted scenario is selected")
	}
	m.cur = make([]int, len(m.files))
	return m, nil
}

func weightOf(tags []string) (int, bool, error) {
	for _, tag := range tags {
		raw, ok := strings.CutPrefix(strings.TrimPrefix(tag, "@"), "weight=")
		if !ok {
			continue
		}
		w, err := strconv.Atoi(raw)
		if err != nil || w < 0 {
			return 0, false, fmt.Errorf("@weight expects a nonnegative integer, got %q", raw)
		}
		return w, true, nil
	}
	return 0, false, nil
}

func (m *mix) next() *feature.File {
	m.mu.Lock()
	defer m.mu.Unlock()
	best := 0
	for i := range m.cur {
		m.cur[i] += m.weight[i]
		if m.cur[i] > m.cur[best] {
			best = i
		}
	}
	m.cur[best] -= m.total
	return m.files[best]
}
