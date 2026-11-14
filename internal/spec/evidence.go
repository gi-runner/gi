package spec

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gi-runner/gi/internal/feature"
)

const evidenceName = ".gi-evidence.json"

// Evidence is one requirement or acceptance criterion and the scenario run that demonstrated it.
// passing and failing belong to this git commit. stale was recorded for another commit.
type Evidence struct {
	ID        string   `json:"id"`
	Text      string   `json:"text,omitempty"`
	Scenarios []string `json:"scenarios,omitempty"`
	Status    string   `json:"status"`
	Commit    string   `json:"commit,omitempty"`
	Ran       string   `json:"ran,omitempty"`
}

// Outcome is one scenario gi test finished.
type Outcome struct {
	File    string
	Name    string
	Passed  bool
	Skipped bool
}

// Head is the git commit of dir. Empty means the tree is not tied to a commit.
var Head = gitHead

var (
	idToken  = regexp.MustCompile(`(?i)^(FR-\d+|AC-\d+(?:-\d+)*)$`)
	idFind   = regexp.MustCompile(`(?i)\b(FR-\d+|AC-\d+(?:-\d+)*)\b`)
	specLine = regexp.MustCompile(`^([A-Za-z0-9_.-]+\.md):(\d+)$`)
)

type link struct {
	id, scenario, file string
}

type store struct {
	Commit string       `json:"commit,omitempty"`
	Ran    string       `json:"ran,omitempty"`
	Items  []storedItem `json:"items"`
}

type storedItem struct {
	ID       string `json:"id"`
	Scenario string `json:"scenario"`
	Status   string `json:"status"`
}

func canonID(id string) string {
	id = strings.TrimPrefix(strings.TrimSpace(id), "@")
	if !idToken.MatchString(id) {
		return ""
	}
	dash := strings.IndexByte(id, '-')
	return strings.ToUpper(id[:dash]) + id[dash:]
}

func findIDs(text string) []string {
	var out []string
	seen := map[string]bool{}
	for _, match := range idFind.FindAllString(text, -1) {
		id := canonID(match)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// Load reads the feature ledger in dir and the evidence recorded for an earlier run.
func Load(dir string) ([]Evidence, error) {
	paths, err := featurePaths(dir)
	if err != nil {
		return nil, err
	}
	return report(paths, nil, false, "")
}

// Record writes the evidence for this run and returns the ledger for those features.
func Record(paths []string, ran []Outcome) ([]Evidence, error) {
	groups := map[string][]string{}
	var dirs []string
	for _, path := range paths {
		dir := filepath.Dir(path)
		if _, ok := groups[dir]; !ok {
			dirs = append(dirs, dir)
		}
		groups[dir] = append(groups[dir], path)
	}
	slices.Sort(dirs)
	ranAt := time.Now().UTC().Format(time.RFC3339)
	var all []Evidence
	var first error
	for _, dir := range dirs {
		ev, err := recordDir(dir, groups[dir], ran, ranAt)
		if err != nil && first == nil {
			first = err
		}
		all = append(all, ev...)
	}
	return all, first
}

// EvidenceNotes lists ledger rows that are not a pass on this commit.
func EvidenceNotes(items []Evidence) []Note {
	var notes []Note
	for _, item := range items {
		if item.Status == "" || item.Status == "passing" {
			continue
		}
		text := item.ID
		switch {
		case item.Status == "uncovered" && item.Text != "":
			text += " " + item.Text
		case len(item.Scenarios) > 0:
			text += " " + strings.Join(item.Scenarios, ", ")
		case item.Text != "":
			text += " " + item.Text
		}
		notes = append(notes, Note{Kind: item.Status, Text: text})
	}
	return notes
}

func recordDir(dir string, paths []string, ran []Outcome, ranAt string) ([]Evidence, error) {
	text, links := readLedger(paths)
	if len(text) == 0 && len(links) == 0 {
		return nil, nil
	}
	head := Head(dir)
	ev := resolve(text, links, indexRuns(ran), store{}, head, true)
	for i := range ev {
		if ev[i].Status == "passing" || ev[i].Status == "failing" {
			ev[i].Commit = head
			ev[i].Ran = ranAt
		}
	}
	err := writeStore(dir, store{Commit: head, Ran: ranAt, Items: executed(links, indexRuns(ran))})
	return ev, err
}

func report(paths []string, ran []Outcome, live bool, ranAt string) ([]Evidence, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	text, links := readLedger(paths)
	if len(text) == 0 && len(links) == 0 {
		return nil, nil
	}
	dir := filepath.Dir(paths[0])
	st, err := readStore(dir)
	if err != nil {
		return nil, err
	}
	ev := resolve(text, links, indexRuns(ran), st, Head(dir), live)
	if live {
		for i := range ev {
			if ev[i].Status == "passing" || ev[i].Status == "failing" {
				ev[i].Commit = Head(dir)
				ev[i].Ran = ranAt
			}
		}
	}
	return ev, nil
}

func featurePaths(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".feature") {
			continue
		}
		paths = append(paths, filepath.Join(dir, entry.Name()))
	}
	slices.Sort(paths)
	return paths, nil
}

func readLedger(paths []string) (map[string]string, []link) {
	text := map[string]string{}
	var links []link
	addID := func(id, body string) {
		if id == "" {
			return
		}
		body = strings.TrimSpace(body)
		if _, ok := text[id]; !ok {
			text[id] = body
			return
		}
		if text[id] == "" && body != "" {
			text[id] = body
		}
	}
	add := func(id, body string) {
		addID(canonID(id), body)
	}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for line := range strings.SplitSeq(string(raw), "\n") {
			body, ok := strings.CutPrefix(strings.TrimSpace(line), "# gi-req:")
			if !ok {
				continue
			}
			body = strings.TrimSpace(body)
			id, rest, _ := strings.Cut(body, " ")
			add(id, rest)
		}
		clean := filepath.Clean(path)
		for _, specLink := range scenarioSpecLines(string(raw), clean) {
			addID(specLink.id, "")
			links = append(links, specLink)
		}
		parsed, err := feature.Parse(path, string(raw))
		if err != nil {
			continue
		}
		for _, sc := range parsed.Scenarios {
			if sc.IsSetup {
				continue
			}
			for _, id := range ownCriterionTags(sc) {
				add(id, "")
				links = append(links, link{id: id, scenario: sc.Name, file: clean})
			}
		}
	}
	return text, links
}

func scenarioSpecLines(raw, file string) []link {
	var out []link
	pending := ""
	for line := range strings.SplitSeq(raw, "\n") {
		trim := strings.TrimSpace(line)
		if id, ok := specSource(trim); ok {
			pending = id
			continue
		}
		if trim == "" || strings.HasPrefix(trim, "@") {
			continue
		}
		name, ok := strings.CutPrefix(trim, "Scenario Outline:")
		if !ok {
			name, ok = strings.CutPrefix(trim, "Scenario:")
		}
		if ok {
			name = strings.TrimSpace(name)
			if pending != "" && name != "" {
				out = append(out, link{id: pending, scenario: name, file: file})
			}
			pending = ""
			continue
		}
		pending = ""
	}
	return out
}

func specSource(line string) (string, bool) {
	line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "#"))
	m := specLine.FindStringSubmatch(line)
	if m == nil {
		return "", false
	}
	return m[1] + ":" + m[2], true
}

func compareEvidenceID(a, b string) int {
	af, an, aok := splitSpecID(a)
	bf, bn, bok := splitSpecID(b)
	if aok && bok {
		if c := strings.Compare(af, bf); c != 0 {
			return c
		}
		return an - bn
	}
	return strings.Compare(a, b)
}

func splitSpecID(id string) (file string, line int, ok bool) {
	m := specLine.FindStringSubmatch(id)
	if m == nil {
		return "", 0, false
	}
	n, err := strconv.Atoi(m[2])
	if err != nil {
		return "", 0, false
	}
	return m[1], n, true
}

func ownCriterionTags(sc feature.Scenario) []string {
	if sc.OwnTags <= 0 || sc.OwnTags > len(sc.Tags) {
		return nil
	}
	tags := sc.Tags[len(sc.Tags)-sc.OwnTags:]
	var out []string
	seen := map[string]bool{}
	for _, tag := range tags {
		id := canonID(tag)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func indexRuns(ran []Outcome) map[string]Outcome {
	out := map[string]Outcome{}
	for _, oc := range ran {
		out[filepath.Clean(oc.File)+"\x00"+oc.Name] = oc
	}
	return out
}

func executed(links []link, ran map[string]Outcome) []storedItem {
	var items []storedItem
	seen := map[string]bool{}
	for _, link := range links {
		oc, ok := ran[link.file+"\x00"+link.scenario]
		if !ok || oc.Skipped {
			continue
		}
		status := "failing"
		if oc.Passed {
			status = "passing"
		}
		key := link.id + "\x00" + link.scenario
		if seen[key] {
			continue
		}
		seen[key] = true
		items = append(items, storedItem{ID: link.id, Scenario: link.scenario, Status: status})
	}
	return items
}

func resolve(text map[string]string, links []link, ran map[string]Outcome, st store, head string, live bool) []Evidence {
	ids := map[string]bool{}
	for id := range text {
		ids[id] = true
	}
	names := map[string][]string{}
	for _, link := range links {
		ids[link.id] = true
		if slices.Contains(names[link.id], link.scenario) {
			continue
		}
		names[link.id] = append(names[link.id], link.scenario)
	}
	var order []string
	for id := range ids {
		order = append(order, id)
	}
	slices.SortFunc(order, compareEvidenceID)
	var out []Evidence
	for _, id := range order {
		item := Evidence{ID: id, Text: text[id], Scenarios: names[id]}
		if live {
			item.Status = statusLive(id, names[id], links, ran)
		} else {
			item.Status, item.Commit, item.Ran = statusStored(id, names[id], st, head)
		}
		out = append(out, item)
	}
	return out
}

func statusLive(id string, scenarios []string, links []link, ran map[string]Outcome) string {
	if len(scenarios) == 0 {
		return "uncovered"
	}
	pass, fail := false, false
	for _, link := range links {
		if link.id != id {
			continue
		}
		oc, ok := ran[link.file+"\x00"+link.scenario]
		if !ok || oc.Skipped {
			continue
		}
		if oc.Passed {
			pass = true
			continue
		}
		fail = true
	}
	if fail {
		return "failing"
	}
	if pass {
		return "passing"
	}
	return "unexecuted"
}

func statusStored(id string, scenarios []string, st store, head string) (status, commit, ran string) {
	if len(scenarios) == 0 {
		return "uncovered", "", ""
	}
	var items []storedItem
	for _, item := range st.Items {
		if item.ID == id {
			items = append(items, item)
		}
	}
	if len(items) == 0 {
		return "unexecuted", "", ""
	}
	if st.Commit == "" || st.Commit != head {
		return "stale", st.Commit, st.Ran
	}
	pass, fail := false, false
	for _, item := range items {
		switch item.Status {
		case "failing":
			fail = true
		case "passing":
			pass = true
		}
	}
	if fail {
		return "failing", st.Commit, st.Ran
	}
	if pass {
		return "passing", st.Commit, st.Ran
	}
	return "unexecuted", "", ""
}

func readStore(dir string) (store, error) {
	raw, err := os.ReadFile(filepath.Join(dir, evidenceName))
	if os.IsNotExist(err) {
		return store{}, nil
	}
	if err != nil {
		return store{}, err
	}
	var st store
	if err := json.Unmarshal(raw, &st); err != nil {
		return store{}, err
	}
	return st, nil
}

func writeStore(dir string, st store) error {
	if st.Items == nil {
		st.Items = []storedItem{}
	}
	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return os.WriteFile(filepath.Join(dir, evidenceName), raw, 0o600)
}

func gitHead(dir string) string {
	cmd := exec.Command("git", "-C", dir, "rev-parse", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
