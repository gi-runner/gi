package engine

import (
	"maps"
	"slices"
	"strings"

	"github.com/gi-runner/gi/internal/feature"
	"github.com/gi-runner/gi/internal/script"
)

// Finding is a step or tag that gi cannot run as written.
type Finding struct {
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Text    string `json:"text"`
	Problem string `json:"problem"`
}

var configKeys = func() map[string]bool {
	keys := map[string]bool{}
	for _, k := range []string{
		"headers", "beforeScenario", "afterScenario", "afterFeature", "afterScenarioOutline", "onStepFailure",
		"timeout", "readTimeout", "url", "ssl", "proxy", "connectTimeout", "localAddress", "retry", "auth",
		"cookies", "charset", "responseHeaders", "callSingleCache", "report", "logging", "driver",
	} {
		keys[k] = true
	}
	for k := range maps.Keys(flags) {
		keys[k] = true
	}
	for k := range maps.Keys(deprecated) {
		keys[k] = true
	}
	return keys
}()

var jvmOnly = []string{"Java.type(", "Java.from(", "Java.to(", "karate.toJava("}

// Check reports steps that no keyword, Go step, or JavaScript can run,
// configure keys gi does not know, JVM-only calls, and tags gi ignores.
func Check(f *feature.File, steps []StepDef) []Finding {
	var out []Finding
	seen := map[int]bool{}
	add := func(line int, text, problem string) {
		if !seen[line] {
			seen[line] = true
			out = append(out, Finding{Path: f.Path, Line: line, Text: text, Problem: problem})
		}
	}
	all := slices.Clone(f.Background)
	for _, sc := range f.Scenarios {
		all = append(all, sc.Steps...)
		for _, tag := range sc.Tags {
			if strings.HasPrefix(strings.TrimPrefix(tag, "@"), "parallel=") {
				add(sc.Line, tag, "@parallel is ignored; use @lock=<name> or @lock=* to serialize scenarios")
			}
		}
	}
	for _, st := range all {
		if problem := checkStep(st, steps); problem != "" {
			add(st.Line, st.Text, problem)
		}
	}
	slices.SortFunc(out, func(a, b Finding) int { return a.Line - b.Line })
	return out
}

func checkStep(st feature.Step, steps []StepDef) string {
	text := strings.TrimSpace(st.Text)
	for _, s := range []string{text, st.DocString} {
		for _, jvm := range jvmOnly {
			if strings.Contains(s, jvm) {
				return strings.TrimSuffix(jvm, "(") + " needs the JVM; register a Go helper with gi.Register instead"
			}
		}
	}
	cmd, rest, _ := strings.Cut(text, " ")
	if Keyword(cmd) {
		if strings.EqualFold(cmd, "configure") {
			name, _, _ := strings.Cut(rest, "=")
			name = strings.TrimSpace(name)
			if hint, ok := deprecated[name]; ok {
				return "configure " + name + " is ignored; " + hint
			}
			if !configKeys[name] {
				return "unknown configure " + name
			}
		}
		return ""
	}
	for _, def := range steps {
		if def.match(text) != nil {
			return ""
		}
	}
	if script.Compiles(orDoc(text, st.DocString)) {
		return ""
	}
	return "not a keyword or JavaScript; define a Go step for it or rewrite it with keywords"
}
