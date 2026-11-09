// Step bodies stay as text. Scenario outlines are expanded here.
package feature

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	gherkin "github.com/cucumber/gherkin/go/v42"
	messages "github.com/cucumber/messages/go/v34"
)

type File struct {
	Path        string
	Name        string
	Description string
	Tags        []string
	Load        LoadSpec
	Profiles    []Profile
	Background  []Step
	Scenarios   []Scenario
	sections    int
}

type LoadSpec struct {
	VUs          int
	VUsTo        int
	UsersToSet   bool
	Rate         float64
	RateSet      bool
	RateTo       float64
	RateToSet    bool
	StageText    []string
	MaxInFlight  int
	MaxEndpoints int
	Duration     time.Duration
	RPS          int
	Warmup       time.Duration
	Start        time.Duration
	Thresholds   []string
}

// Profile is one named load shape inside a feature file.
// Scenarios opt in with @load=<name>.
type Profile struct {
	Name string
	Load LoadSpec
}

type Scenario struct {
	Name        string
	Description string
	Line        int
	Tags        []string
	OwnTags     int // trailing entries of Tags declared below the feature
	Steps       []Step
	Lead        int // inlined background steps; @setup runs the rest
	IsSetup     bool
	SetupName   string
	Outline     string // single-column examples header, expanded at run time
	Row         map[string]any
	Num         int
	OutlineID   int
	Section     int
	Examples    *Examples
}

// Examples describes the outline a scenario was expanded from.
type Examples struct {
	Name        string
	Description string
	Line        int
	Tables      []map[string]any
	Scenarios   int
}

func (f *File) Setup(name string) (Scenario, bool) {
	for _, sc := range f.Scenarios {
		if !sc.IsSetup {
			continue
		}
		if name == "" || setupHas(sc.SetupName, name) {
			return sc, true
		}
	}
	return Scenario{}, false
}

func setupHas(raw, name string) bool {
	for part := range strings.SplitSeq(raw, ",") {
		if strings.TrimSpace(part) == name {
			return true
		}
	}
	return false
}

type Step struct {
	Keyword   string
	Text      string
	DocString string
	Rows      [][]string
	Line      int
	Label     string
}

func AllowsEnv(tags []string, env string) bool {
	saw, match := false, false
	for _, t := range tags {
		k, v, ok := strings.Cut(strings.TrimPrefix(t, "@"), "=")
		if !ok {
			continue
		}
		switch k {
		case "envnot":
			for name := range strings.SplitSeq(v, ",") {
				if strings.TrimSpace(name) == env {
					return false
				}
			}
		case "env":
			saw = true
			for name := range strings.SplitSeq(v, ",") {
				if strings.TrimSpace(name) == env {
					match = true
				}
			}
		}
	}
	return !saw || match
}

func HasTag(tags []string, name string) bool {
	name = strings.TrimPrefix(name, "@")
	for _, t := range tags {
		if strings.TrimPrefix(t, "@") == name {
			return true
		}
	}
	return false
}

func ParseFile(path string) (*File, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(path, string(b))
}

func Parse(path, source string) (*File, error) {
	id := 0
	doc, err := gherkin.ParseGherkinDocument(strings.NewReader(source), func() string {
		id++
		return strconv.Itoa(id)
	})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if doc == nil || doc.Feature == nil {
		return nil, fmt.Errorf("%s: no feature found", path)
	}
	f := &File{
		Path:        path,
		Name:        doc.Feature.Name,
		Description: strings.TrimSpace(doc.Feature.Description),
		Tags:        tagNames(doc.Feature.Tags),
	}
	spec := &f.Load
	for _, c := range doc.Comments {
		next, err := f.readLoad(spec, c.Text)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, c.Location.Line, err)
		}
		spec = next
	}
	if err := f.Load.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	for i := range f.Profiles {
		if err := f.Profiles[i].Load.validate(); err != nil {
			return nil, fmt.Errorf("%s: profile %s: %w", path, f.Profiles[i].Name, err)
		}
	}
	labels := commentLabels(doc.Comments)
	if err := walkFeature(doc.Feature, f, nil, labels); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := f.checkLoadTags(); err != nil {
		return nil, err
	}
	return f, nil
}

func walkFeature(feat *messages.Feature, f *File, extraBack []Step, labels map[int64]string) error {
	var background []Step
	for _, child := range feat.Children {
		switch {
		case child.Background != nil:
			background = stepsFrom(child.Background.Steps, labels)
		case child.Scenario != nil:
			back := append(append([]Step{}, extraBack...), background...)
			if err := addScenario(f, child.Scenario, back, labels); err != nil {
				return err
			}
		case child.Rule != nil:
			var ruleBack []Step
			for _, rc := range child.Rule.Children {
				if rc.Background != nil {
					ruleBack = stepsFrom(rc.Background.Steps, labels)
				}
			}
			for _, rc := range child.Rule.Children {
				if rc.Scenario == nil {
					continue
				}
				back := append(append([]Step{}, extraBack...), background...)
				back = append(back, ruleBack...)
				if err := addScenario(f, rc.Scenario, back, labels); err != nil {
					return err
				}
			}
		}
	}
	if f.Background == nil {
		f.Background = background
	}
	return nil
}

func addScenario(f *File, sc *messages.Scenario, background []Step, labels map[int64]string) error {
	tags := append(append([]string{}, f.Tags...), tagNames(sc.Tags)...)
	own := stepsFrom(sc.Steps, labels)
	steps := append(append([]Step{}, background...), own...)
	isSetup, setupName := setupTag(sc.Tags)
	section := f.sections
	f.sections++
	base := Scenario{
		Name:        sc.Name,
		Description: strings.TrimSpace(sc.Description),
		Line:        int(sc.Location.Line),
		Tags:        tags,
		OwnTags:     len(tags) - len(f.Tags),
		Steps:       steps,
		Lead:        len(background),
		IsSetup:     isSetup,
		SetupName:   setupName,
		Section:     section,
	}
	if len(sc.Examples) == 0 {
		f.Scenarios = append(f.Scenarios, base)
		return nil
	}
	examples := outlineOf(sc)
	base.Examples = examples
	base.OutlineID = base.Line
	for _, ex := range sc.Examples {
		if ex.TableHeader == nil {
			continue
		}
		headers := cells(ex.TableHeader)
		exTags := append(append([]string{}, tags...), tagNames(ex.Tags)...)
		if expr, ok := outlineExpr(headers); ok || (len(headers) == 1 && len(ex.TableBody) == 0) {
			if !ok {
				expr = strings.TrimSpace(headers[0])
			}
			dynamic := base
			dynamic.Tags = exTags
			dynamic.OwnTags = len(exTags) - len(f.Tags)
			dynamic.Outline = expr
			f.Scenarios = append(f.Scenarios, dynamic)
			continue
		}
		n := 0
		for _, row := range ex.TableBody {
			n++
			vals := cells(row)
			repl := map[string]string{}
			var label []string
			for i, h := range headers {
				v := ""
				if i < len(vals) {
					v = vals[i]
				}
				repl[h] = v
				label = append(label, h+"="+v)
			}
			expanded := base
			expanded.Name = sc.Name + " [" + strings.Join(label, ", ") + "]"
			expanded.Line = int(row.Location.Line)
			expanded.Tags = exTags
			expanded.OwnTags = len(exTags) - len(f.Tags)
			expanded.Steps = substitute(steps, repl)
			expanded.Row = rowValues(repl)
			expanded.Num = n - 1
			f.Scenarios = append(f.Scenarios, expanded)
		}
	}
	return nil
}

func outlineOf(sc *messages.Scenario) *Examples {
	out := &Examples{Name: sc.Name, Description: strings.TrimSpace(sc.Description), Line: int(sc.Location.Line)}
	for _, ex := range sc.Examples {
		if ex.TableHeader == nil {
			continue
		}
		headers := cells(ex.TableHeader)
		rows := make([]any, 0, len(ex.TableBody))
		for _, row := range ex.TableBody {
			vals := cells(row)
			m := make(map[string]any, len(headers))
			for i, h := range headers {
				m[h] = ""
				if i < len(vals) {
					m[h] = vals[i]
				}
			}
			rows = append(rows, m)
		}
		out.Tables = append(out.Tables, map[string]any{"tags": tagNames(ex.Tags), "data": rows})
		out.Scenarios += len(rows)
	}
	return out
}

func outlineExpr(headers []string) (string, bool) {
	if len(headers) != 1 {
		return "", false
	}
	h := strings.TrimSpace(headers[0])
	if strings.Contains(h, "(") {
		return h, true
	}
	return "", false
}

func Materialize(sc Scenario, rows []map[string]any) ([]Scenario, error) {
	out := make([]Scenario, 0, len(rows))
	var examples *Examples
	if sc.Examples != nil {
		copied := *sc.Examples
		copied.Scenarios = len(rows)
		examples = &copied
	}
	for i, row := range rows {
		repl := make(map[string]string, len(row))
		for k, v := range row {
			if text, ok := v.(string); ok {
				repl[k] = text
			} else {
				raw, err := json.Marshal(v)
				if err != nil {
					return nil, fmt.Errorf("example column %s: %w", k, err)
				}
				repl[k] = string(raw)
			}
		}
		next := sc
		next.Name = sc.Name + " [" + rowLabel(repl) + "]"
		next.Tags = append([]string{}, sc.Tags...)
		next.Steps = substitute(sc.Steps, repl)
		next.Outline = ""
		next.Row = row
		next.Num = i
		next.Examples = examples
		out = append(out, next)
	}
	return out, nil
}

func rowLabel(row map[string]string) string {
	keys := make([]string, 0, len(row))
	for k := range row {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + "=" + row[k]
	}
	return strings.Join(parts, ", ")
}

func substitute(steps []Step, repl map[string]string) []Step {
	keys := make([]string, 0, len(repl))
	for k := range repl {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
	out := make([]Step, len(steps))
	for i, st := range steps {
		out[i] = st
		out[i].Text = applyRepl(st.Text, keys, repl)
		out[i].DocString = applyRepl(st.DocString, keys, repl)
		out[i].Rows = applyRows(st.Rows, keys, repl)
	}
	return out
}

func applyRepl(s string, keys []string, repl map[string]string) string {
	for _, k := range keys {
		s = strings.ReplaceAll(s, "<"+k+">", repl[k])
	}
	return s
}

func applyRows(rows [][]string, keys []string, repl map[string]string) [][]string {
	if len(rows) == 0 {
		return nil
	}
	out := make([][]string, len(rows))
	for i, row := range rows {
		out[i] = make([]string, len(row))
		for j, cell := range row {
			out[i][j] = applyRepl(cell, keys, repl)
		}
	}
	return out
}

func stepsFrom(in []*messages.Step, labels map[int64]string) []Step {
	out := make([]Step, 0, len(in))
	var prev int64
	for _, st := range in {
		doc := ""
		if st.DocString != nil {
			doc = st.DocString.Content
		}
		var rows [][]string
		if st.DataTable != nil {
			rows = tableRows(st.DataTable)
		}
		label := ""
		if labels != nil {
			var best int64
			for line, text := range labels {
				if line < st.Location.Line && line > prev && line > best {
					best = line
					label = text
				}
			}
		}
		out = append(out, Step{
			Keyword:   strings.TrimSpace(st.Keyword),
			Text:      st.Text,
			DocString: doc,
			Rows:      rows,
			Line:      int(st.Location.Line),
			Label:     label,
		})
		prev = st.Location.Line
	}
	return out
}

func tableRows(t *messages.DataTable) [][]string {
	out := make([][]string, 0, len(t.Rows))
	for _, row := range t.Rows {
		if row == nil {
			continue
		}
		cells := make([]string, len(row.Cells))
		for i, c := range row.Cells {
			if c != nil {
				cells[i] = c.Value
			}
		}
		out = append(out, cells)
	}
	return out
}

func cells(row *messages.TableRow) []string {
	out := make([]string, len(row.Cells))
	for i, c := range row.Cells {
		out[i] = c.Value
	}
	return out
}

func setupTag(tags []*messages.Tag) (bool, string) {
	for _, t := range tags {
		k, v, ok := strings.Cut(strings.TrimPrefix(t.Name, "@"), "=")
		if k != "setup" {
			continue
		}
		if !ok {
			return true, ""
		}
		return true, v
	}
	return false, ""
}

func tagNames(tags []*messages.Tag) []string {
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		out = append(out, t.Name)
	}
	return out
}

func commentLabels(comments []*messages.Comment) map[int64]string {
	out := map[int64]string{}
	for _, c := range comments {
		text := strings.TrimSpace(c.Text)
		text = strings.TrimPrefix(text, "#")
		text = strings.TrimSpace(text)
		if text == "" || strings.HasPrefix(text, "gi-load:") {
			continue
		}
		out[c.Location.Line] = text
	}
	return out
}

func (f *File) readLoad(spec *LoadSpec, raw string) (*LoadSpec, error) {
	text := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), "#"))
	rest, ok := strings.CutPrefix(text, "gi-load:")
	if !ok {
		return spec, nil
	}
	fields := strings.Fields(rest)
	if len(fields) > 0 && strings.HasPrefix(fields[0], "name=") {
		name := strings.TrimPrefix(fields[0], "name=")
		if !profileName(name) {
			return spec, fmt.Errorf("profile name %q must start with a letter", name)
		}
		for _, p := range f.Profiles {
			if p.Name == name {
				return spec, fmt.Errorf("profile %q is repeated", name)
			}
		}
		f.Profiles = append(f.Profiles, Profile{Name: name})
		spec = &f.Profiles[len(f.Profiles)-1].Load
		fields = fields[1:]
		if len(fields) == 0 {
			return spec, nil
		}
		rest = strings.Join(fields, " ")
	}
	return spec, applyLoadComment(spec, "# gi-load: "+rest)
}

func profileName(name string) bool {
	if name == "" || !isLetter(rune(name[0])) {
		return false
	}
	for _, r := range name {
		if !isLetter(r) && !isDigit(r) && r != '_' && r != '-' {
			return false
		}
	}
	return true
}

func isLetter(r rune) bool { return r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' }
func isDigit(r rune) bool  { return r >= '0' && r <= '9' }

// Plans is the unnamed profile, when it has settings, then every named profile.
// A feature with no gi-load comments still has one empty plan.
func (f *File) Plans() []Profile {
	if len(f.Profiles) == 0 {
		return []Profile{{Load: f.Load}}
	}
	out := append([]Profile(nil), f.Profiles...)
	if f.Load.set() {
		out = append([]Profile{{Load: f.Load}}, out...)
	}
	return out
}

// ForProfile copies the file down to the scenarios that belong to name.
// An empty name is the unnamed profile. With no @load tags, every profile runs every scenario.
func (f *File) ForProfile(name string) *File {
	g := *f
	g.Profiles = nil
	g.Load = f.Load.Clone()
	for _, p := range f.Profiles {
		if p.Name == name {
			g.Load = p.Load.Clone()
		}
	}
	if !f.hasLoadTags() {
		g.Scenarios = append([]Scenario(nil), f.Scenarios...)
		return &g
	}
	shareUntagged := name != "" && !f.Load.set()
	var keep []Scenario
	for _, sc := range f.Scenarios {
		tags := loadTagNames(sc.Tags)
		switch {
		case sc.IsSetup:
			keep = append(keep, sc)
		case len(tags) == 0 && (name == "" || shareUntagged):
			keep = append(keep, sc)
		case slices.Contains(tags, name):
			keep = append(keep, sc)
		}
	}
	g.Scenarios = keep
	return &g
}

func (f *File) checkLoadTags() error {
	known := map[string]bool{"": len(f.Profiles) == 0 || f.Load.set()}
	for _, p := range f.Profiles {
		known[p.Name] = true
	}
	for _, sc := range f.Scenarios {
		for _, name := range loadTagNames(sc.Tags) {
			if !known[name] {
				return fmt.Errorf("%s:%d: @load=%s has no gi-load profile", f.Path, sc.Line, name)
			}
		}
	}
	return nil
}

func (f *File) hasLoadTags() bool {
	for _, sc := range f.Scenarios {
		if len(loadTagNames(sc.Tags)) > 0 {
			return true
		}
	}
	return false
}

func loadTagNames(tags []string) []string {
	var out []string
	for _, t := range tags {
		k, v, ok := strings.Cut(strings.TrimPrefix(t, "@"), "=")
		if ok && k == "load" && v != "" {
			out = append(out, v)
		}
	}
	return out
}

func (spec LoadSpec) set() bool {
	return spec.VUs > 0 || spec.UsersToSet || spec.RateSet || spec.Rate > 0 || spec.RateToSet ||
		len(spec.StageText) > 0 || spec.Duration > 0 || spec.RPS > 0 || spec.Warmup > 0 || spec.Start > 0 ||
		spec.MaxInFlight > 0 || spec.MaxEndpoints > 0 || len(spec.Thresholds) > 0
}

func applyLoadComment(spec *LoadSpec, raw string) error {
	text := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), "#"))
	rest, ok := strings.CutPrefix(text, "gi-load:")
	if !ok {
		return nil
	}
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return errors.New("gi-load requires settings")
	}
	switch fields[0] {
	case "stage", "threshold":
		body := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(rest), fields[0]))
		if body == "" {
			return fmt.Errorf("gi-load %s requires a value", fields[0])
		}
		if fields[0] == "stage" {
			spec.StageText = append(spec.StageText, body)
		} else {
			spec.Thresholds = append(spec.Thresholds, body)
		}
		return nil
	}
	for _, field := range fields {
		key, raw, ok := strings.Cut(field, "=")
		if !ok {
			return fmt.Errorf("gi-load setting %q requires name=value", field)
		}
		var err error
		switch strings.ToLower(key) {
		case "vus", "vu":
			spec.VUs, err = strconv.Atoi(raw)
		case "vusto":
			spec.VUsTo, err = strconv.Atoi(raw)
			spec.UsersToSet = true
		case "duration":
			spec.Duration, err = time.ParseDuration(raw)
			if err == nil && spec.Duration <= 0 {
				err = errors.New("duration must be positive")
			}
		case "rps":
			spec.RPS, err = strconv.Atoi(raw)
		case "rate":
			spec.Rate, err = strconv.ParseFloat(raw, 64)
			spec.RateSet = true
		case "rateto":
			spec.RateTo, err = strconv.ParseFloat(raw, 64)
			spec.RateToSet = true
		case "warmup":
			spec.Warmup, err = time.ParseDuration(raw)
		case "start":
			spec.Start, err = time.ParseDuration(raw)
			if err == nil && spec.Start < 0 {
				err = errors.New("start must be nonnegative")
			}
		case "maxendpoints":
			spec.MaxEndpoints, err = strconv.Atoi(raw)
			if err == nil && spec.MaxEndpoints <= 0 {
				err = errors.New("maxEndpoints must be positive")
			}
		case "maxinflight":
			spec.MaxInFlight, err = strconv.Atoi(raw)
		default:
			return fmt.Errorf("unknown gi-load setting %q", key)
		}
		if err != nil {
			return fmt.Errorf("gi-load %s: %w", key, err)
		}
	}
	return nil
}

// Clone copies the profile so a run can adjust it without touching the file.
func (spec LoadSpec) Clone() LoadSpec {
	spec.StageText = append([]string(nil), spec.StageText...)
	spec.Thresholds = append([]string(nil), spec.Thresholds...)
	return spec
}

// WithStages returns the profile with stage text replacing users, rate, and duration.
func (spec LoadSpec) WithStages(text []string) LoadSpec {
	spec.VUs, spec.VUsTo, spec.UsersToSet = 0, 0, false
	spec.Rate, spec.RateTo, spec.RateSet, spec.RateToSet = 0, 0, false, false
	spec.Duration = 0
	spec.StageText = append([]string(nil), text...)
	return spec
}

func (spec LoadSpec) validate() error {
	if spec.Start < 0 || spec.Warmup < 0 || spec.VUs < 0 || spec.VUsTo < 0 || spec.RPS < 0 || spec.Rate < 0 || spec.RateTo < 0 ||
		math.IsNaN(spec.Rate) || math.IsInf(spec.Rate, 0) || math.IsNaN(spec.RateTo) || math.IsInf(spec.RateTo, 0) {
		return errors.New("gi-load values must be finite and nonnegative")
	}
	if (spec.VUs > 0 || spec.UsersToSet) && (spec.RateSet || spec.Rate > 0 || spec.RateToSet) {
		return errors.New("gi-load sets both virtual users and a start rate")
	}
	if len(spec.StageText) > 0 && (spec.VUs > 0 || spec.UsersToSet || spec.RateSet || spec.Rate > 0 || spec.RateToSet) {
		return errors.New("gi-load stages replace virtual users and a start rate")
	}
	if spec.MaxInFlight < 0 {
		return errors.New("gi-load values must be finite and nonnegative")
	}
	if spec.RateSet || spec.RateToSet {
		to := spec.Rate
		if spec.RateToSet {
			to = spec.RateTo
		}
		if spec.Rate == 0 && to == 0 {
			return errors.New("start rate is zero")
		}
	}
	return nil
}

func rowValues(row map[string]string) map[string]any {
	out := make(map[string]any, len(row))
	for k, v := range row {
		out[k] = v
	}
	return out
}
