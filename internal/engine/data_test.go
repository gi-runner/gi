package engine_test

import (
	"context"
	"strings"
	"testing"

	"github.com/gi-runner/gi/internal/engine"
	"github.com/gi-runner/gi/internal/feature"
)

func TestDataSteps(t *testing.T) {
	const src = `
Feature:

Scenario: shape data
* def name = 'Ada'
* text raw = name
* match raw == 'name'
* text greeting =
"""
hello <name>
"""
* replace greeting.name = name
* match greeting == 'hello Ada'
* text line = a <b> c
* replace line
  | token | value |
  | b | 'B' |
* match line == 'a B c'
* json user = { name: '#(name)', age: 36 }
* def rawJson = '{"ok":true}'
* json parsed = rawJson
* match parsed.ok == true
* copy other = user
* set other.age = 1
* match user.age == 36
* match other.age == 1
* table people
  | name | age |
  | name | 36 |
  | 'Bob' | 20 |
  | 'C' | |
  | 'D' | (null) |
* match people[0].name == 'Ada'
* match people[1].age == 20
* match people[2].age == '#notpresent'
* match people[3].age == '#null'
* assert people[1].age == 20

Scenario: assert fails
* assert 1 == 2
`
	f, err := feature.Parse("data.feature", src)
	if err != nil {
		t.Fatal(err)
	}
	res, err := engine.RunFile(context.Background(), f, engine.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Scenarios) != 2 {
		t.Fatalf("scenarios: %d", len(res.Scenarios))
	}
	if !res.Scenarios[0].Passed {
		t.Fatal(res.Scenarios[0].Error)
	}
	if res.Scenarios[1].Passed || !strings.Contains(res.Scenarios[1].Error, "assert failed") {
		t.Fatalf("expected assert failure, got %#v", res.Scenarios[1])
	}
}
