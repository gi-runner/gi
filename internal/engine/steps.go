package engine

import (
	"context"
	"fmt"
	"maps"
	"regexp"

	"github.com/gi-runner/gi/internal/feature"
)

// StepDef runs Go code for step text that no built-in keyword claims.
// Pattern must match the whole text; its groups become args.
type StepDef struct {
	Pattern *regexp.Regexp
	Run     func(ctx context.Context, sc *Scope, args []string) error
}

// Scope gives a Go step the scenario's variables and the step's attachments.
type Scope struct {
	e  *exec
	st feature.Step
}

// match returns the submatches when the pattern covers the whole text.
func (d StepDef) match(text string) []string {
	m := d.Pattern.FindStringSubmatch(text)
	if m == nil || len(m[0]) != len(text) {
		return nil
	}
	return m
}

// Get reads a scenario variable as plain Go data.
func (s *Scope) Get(name string) (any, bool) { return s.e.vm.Get(name) }

// Set defines a scenario variable, as def does.
func (s *Scope) Set(name string, val any) { s.e.setDefined(name, val) }

// DocString returns the step's doc string, or "".
func (s *Scope) DocString() string { return s.st.DocString }

// Table returns the step's data table rows, header included.
func (s *Scope) Table() [][]string { return s.st.Rows }

func (e *exec) customStep(ctx context.Context, text string, st feature.Step) (bool, error) {
	for _, def := range e.opt.Steps {
		if m := def.match(text); m != nil {
			return true, def.Run(ctx, &Scope{e: e, st: st}, m[1:])
		}
	}
	return false, nil
}

// helpersFor overlays per-run helpers on the registered ones.
func (e *exec) helpersFor() map[string]func([]any) (any, error) {
	out := registeredHelpers()
	maps.Copy(out, e.opt.Helpers)
	return out
}

// CheckHelpers rejects per-run helper names that a built-in owns.
func CheckHelpers(h map[string]func([]any) (any, error)) error {
	for name, fn := range h {
		if fn == nil || !ident(name) || builtinHelper(name) {
			return fmt.Errorf("helper %q is not available for registration", name)
		}
	}
	return nil
}
