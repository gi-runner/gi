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

func TestMalformedLoadSettingsDoNotFallBackToDefaults(t *testing.T) {
	for _, setting := range []string{"rate=oops", "vus=99999999999999999999999", "duration=forever", "duration=0s", "warmup=-1s", "rps=no", "maxInflight=oops", "rates=1000", "rate", "stage", "threshold", ""} {
		_, err := Parse("bad-load.feature", "# gi-load: "+setting+"\nFeature: bad load\nScenario: never runs\n* assert true\n")
		if err == nil {
			t.Errorf("silently accepted gi-load: %s", setting)
		}
	}
}

func TestEndpointLimitComment(t *testing.T) {
	f, err := Parse("limits.feature", "# gi-load: maxEndpoints=3\nFeature: limits\nScenario: ok\n* assert true\n")
	if err != nil || f.Load.MaxEndpoints != 3 {
		t.Fatalf("endpoint limit: %v %v", f, err)
	}
	for _, value := range []string{"0", "-1", "oops"} {
		if _, err := Parse("limits.feature", "# gi-load: maxEndpoints="+value+"\nFeature: limits\nScenario: ok\n* assert true\n"); err == nil {
			t.Fatalf("accepted maxEndpoints=%s", value)
		}
	}
}
