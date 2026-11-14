package spec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gi-runner/gi/internal/feature"
)

func TestScenarioTagIsEvidence(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "spec.md"), `
### User Story 1 - Pay (Priority: P1)

**Acceptance Scenarios**:

1. **AC-016-01** **Given** a client, **When** POST /pay, **Then** status 201

## Requirements

- **FR-016**: Payments are processed exactly once.
- **AC-016-02**: Duplicate delivery does not create another payment.
`)
	result, err := Compile(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	text := readFile(t, result.Files[0].Path)
	parsed, err := feature.Parse(result.Files[0].Path, text)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Scenarios) != 1 || !feature.HasTag(ownOnly(parsed.Scenarios[0]), "AC-016-01") || feature.HasTag(ownOnly(parsed.Scenarios[0]), "FR-016") {
		t.Fatalf("scenario tags: %+v", parsed.Scenarios[0].Tags)
	}
	got := map[string]Evidence{}
	for _, item := range result.Evidence {
		got[item.ID] = item
	}
	if got["AC-016-01"].Status != "unexecuted" || len(got["AC-016-01"].Scenarios) != 1 {
		t.Fatalf("AC-016-01: %+v", got["AC-016-01"])
	}
	if got["AC-016-02"].Status != "uncovered" || got["FR-016"].Status != "uncovered" {
		t.Fatalf("ledger: %+v", result.Evidence)
	}
	var line Evidence
	for _, item := range result.Evidence {
		if strings.HasPrefix(item.ID, "spec.md:") {
			line = item
		}
	}
	if line.Status != "unexecuted" || len(line.Scenarios) != 1 {
		t.Fatalf("acceptance line: %+v", result.Evidence)
	}
}

func TestEvidenceFollowsTheCommit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pay.feature")
	body := "# gi-req: FR-016 Payments are processed exactly once.\n" +
		"# gi-req: AC-016-02 Duplicate delivery does not create another payment.\n" +
		"Feature: Pay\n\n" +
		"@AC-016-01\n" +
		"Scenario: Process a payment successfully\n" +
		"  * assert true\n\n" +
		"@AC-016-02\n" +
		"Scenario: Duplicate event does not create another payment\n" +
		"  * assert false\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	prev := Head
	Head = func(string) string { return "abc" }
	t.Cleanup(func() { Head = prev })

	ran := []Outcome{
		{File: path, Name: "Process a payment successfully", Passed: true},
		{File: path, Name: "Duplicate event does not create another payment", Passed: false},
	}
	ev, err := Record([]string{path}, ran)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, item := range ev {
		got[item.ID] = item.Status
		if item.Status == "passing" && item.Commit != "abc" {
			t.Fatalf("fresh pass is not tied to the commit: %+v", item)
		}
	}
	if got["AC-016-01"] != "passing" || got["AC-016-02"] != "failing" || got["FR-016"] != "uncovered" {
		t.Fatalf("live ledger: %+v", ev)
	}

	Head = func(string) string { return "def" }
	loaded, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range loaded {
		if item.ID == "FR-016" {
			if item.Status != "uncovered" {
				t.Fatalf("FR-016: %+v", item)
			}
			continue
		}
		if item.Status != "stale" || item.Commit != "abc" {
			t.Fatalf("stale: %+v", item)
		}
	}

	Head = func(string) string { return "abc" }
	same, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	again := map[string]string{}
	for _, item := range same {
		again[item.ID] = item.Status
	}
	if again["AC-016-01"] != "passing" || again["AC-016-02"] != "failing" {
		t.Fatalf("same commit: %+v", same)
	}
}

func TestOwnCriterionTagParses(t *testing.T) {
	body := "# gi-req: AC-9 Payment audit event\nFeature: Pay\n\n@AC-1\nScenario: paid\n  * assert true\n"
	parsed, err := feature.Parse("pay.feature", body)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Scenarios) != 1 {
		t.Fatalf("scenarios: %+v", parsed.Scenarios)
	}
	sc := parsed.Scenarios[0]
	if tags := ownCriterionTags(sc); len(tags) != 1 || tags[0] != "AC-1" {
		t.Fatalf("tags %v own %d all %v", tags, sc.OwnTags, sc.Tags)
	}
}

func ownOnly(sc feature.Scenario) []string {
	if sc.OwnTags <= 0 || sc.OwnTags > len(sc.Tags) {
		return nil
	}
	return sc.Tags[len(sc.Tags)-sc.OwnTags:]
}
