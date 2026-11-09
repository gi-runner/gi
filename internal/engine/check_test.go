package engine_test

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/gi-runner/gi/internal/engine"
	"github.com/gi-runner/gi/internal/feature"
)

func TestCheck(t *testing.T) {
	f, err := feature.Parse("check.feature", `Feature: check
Background:
* def base = 'x'

@parallel=false
Scenario: findings
Given I log in as admin
And the cart has 3 items
* def j = Java.type('demo.Util')
* configure ntlmAuth = {}
* configure printEnabled = false
* configure retry = { count: 1 }
* def a = 1
* a + 1
* match a == 1
`)
	if err != nil {
		t.Fatal(err)
	}
	steps := []engine.StepDef{{Pattern: regexp.MustCompile(`the cart has (\d+) items`), Run: func(context.Context, *engine.Scope, []string) error { return nil }}}
	got := map[int]string{}
	for _, fd := range engine.Check(f, steps) {
		got[fd.Line] = fd.Problem
	}
	want := map[int]string{6: "@parallel", 7: "not a keyword", 9: "Java.type", 10: "unknown configure ntlmAuth", 11: "is ignored"}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for line, part := range want {
		if !strings.Contains(got[line], part) {
			t.Errorf("line %d: %q lacks %q", line, got[line], part)
		}
	}
}

func TestCheckKnowsKarateConfigureKeys(t *testing.T) {
	keys := "ssl proxy readTimeout connectTimeout followRedirects strictResponseParsing localAddress charset auth url headers cookies " +
		"retry httpRetryEnabled report logging callSingleCache continueOnStepFailure abortedStepsShouldPass abortSuiteOnFailure " +
		"matchEachEmptyAllowed cors responseHeaders beforeScenario afterScenario afterScenarioOutline afterFeature onStepFailure " +
		"driver logPrettyRequest logPrettyResponse printEnabled lowerCaseResponseHeaders logModifier timeout"
	var src strings.Builder
	src.WriteString("Feature: keys\nScenario: keys\n")
	for key := range strings.FieldsSeq(keys) {
		src.WriteString("* configure " + key + " = null\n")
	}
	f, err := feature.Parse("keys.feature", src.String())
	if err != nil {
		t.Fatal(err)
	}
	for _, fd := range engine.Check(f, nil) {
		if strings.Contains(fd.Problem, "unknown") {
			t.Errorf("%s", fd.Text)
		}
	}
}
