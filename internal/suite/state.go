package suite

import (
	"fmt"
	"io"
)

// Warn writes msg to w the first time key is seen in this run.
func (s *Suite) Warn(w io.Writer, key, msg string) {
	if s == nil || w == nil {
		return
	}
	s.mu.Lock()
	seen := s.warned[key]
	if s.warned == nil {
		s.warned = map[string]bool{}
	}
	s.warned[key] = true
	s.mu.Unlock()
	if !seen {
		_, _ = fmt.Fprintln(w, "warning:", msg)
	}
}

// Abort makes Aborted report true for the rest of the run.
func (s *Suite) Abort() {
	if s != nil {
		s.aborted.Store(true)
	}
}

// Aborted reports whether a scenario stopped the run with abortSuiteOnFailure.
func (s *Suite) Aborted() bool { return s != nil && s.aborted.Load() }
