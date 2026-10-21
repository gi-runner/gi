package feature

import "testing"

func TestLoadComments(t *testing.T) {
	f, err := Parse("load.feature", `# gi-load: vus=1 vusTo=40 duration=30s warmup=1s rps=5
# gi-load: threshold p95 < 200ms
Feature: users
Scenario: ok
* assert true
`)
	if err != nil {
		t.Fatal(err)
	}
	if f.Load.VUs != 1 || !f.Load.UsersToSet || f.Load.VUsTo != 40 || f.Load.Duration.String() != "30s" || f.Load.Warmup.String() != "1s" || f.Load.RPS != 5 || len(f.Load.Thresholds) != 1 {
		t.Fatalf("%+v", f.Load)
	}
	f, err = Parse("rate.feature", "# gi-load: rate=10 rateTo=200 duration=5s\nFeature: rate\nScenario: ok\n* assert true\n")
	if err != nil {
		t.Fatal(err)
	}
	if f.Load.Rate != 10 || !f.Load.RateToSet || f.Load.RateTo != 200 {
		t.Fatalf("%+v", f.Load)
	}
	if _, err := Parse("mixed.feature", "# gi-load: vus=2 rate=10\nFeature: mixed\nScenario: ok\n* assert true\n"); err == nil {
		t.Fatal("accepted users and a start rate")
	}
	if _, err := Parse("zero.feature", "# gi-load: rate=0\nFeature: zero\nScenario: ok\n* assert true\n"); err == nil {
		t.Fatal("accepted a zero start rate")
	}
	f, err = Parse("stages.feature", `# gi-load: stage rate=10 duration=30s
# gi-load: stage spike rate=200 duration=10s
# gi-load: maxInflight=25
Feature: stages
Scenario: ok
* assert true
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Load.StageText) != 2 || f.Load.StageText[1] != "spike rate=200 duration=10s" || f.Load.MaxInFlight != 25 {
		t.Fatalf("%+v", f.Load)
	}
}
