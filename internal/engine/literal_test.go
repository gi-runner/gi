package engine_test

import (
	"context"
	"testing"

	"github.com/gi-runner/gi/internal/engine"
	"github.com/gi-runner/gi/internal/feature"
)

func TestYamlCsvAndTags(t *testing.T) {
	src := `
Feature:

@smoke
Scenario: only smoke
* def n = 1

@smoke @fast
Scenario: both
* yaml user =
"""
name: Ada
age: 36
"""
* match user.name == 'Ada'
* match user.age == 36
* csv rows =
"""
name,age
Ada,36
"""
* match rows[0].name == 'Ada'
* match rows[0].age == '36'

@fast
Scenario: only fast
* def n = 1
`
	f, err := feature.Parse("data.feature", src)
	if err != nil {
		t.Fatal(err)
	}
	res, err := engine.RunFile(context.Background(), f, engine.Options{Tag: "@smoke and @fast"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Scenarios) != 3 {
		t.Fatalf("scenarios = %d", len(res.Scenarios))
	}
	if !res.Scenarios[0].Skipped || !res.Scenarios[2].Skipped {
		t.Fatalf("filters: %+v %+v", res.Scenarios[0].Skipped, res.Scenarios[2].Skipped)
	}
	if !res.Scenarios[1].Passed {
		t.Fatal(res.Scenarios[1].Error)
	}
}
