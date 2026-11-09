package load

import (
	"math"
	"testing"
	"time"
)

func TestShareSplitsUsersRateAndCaps(t *testing.T) {
	cfg := Config{
		RPS: 10, MaxInFlight: 4, Duration: time.Second, Thresholds: []string{"p95 < 1s"},
		Stages: []Stage{{From: 10, To: 40, ToSet: true, Duration: time.Second}},
	}
	var users int
	var rateSum, toSum float64
	for i := range 3 {
		part, err := Share(cfg, i, 3)
		if err != nil {
			t.Fatal(err)
		}
		if part.Thresholds != nil || part.RPS < 1 {
			t.Fatalf("part %d = %+v", i, part)
		}
		users += int(part.Stages[0].From)
	}
	if users != 10 {
		t.Fatalf("users = %d", users)
	}
	rateCfg := cfg
	rateCfg.Stages = []Stage{{Rate: true, From: 10, To: 20, ToSet: true, Duration: time.Second}}
	rateCfg.RPS = 0
	rateCfg.MaxInFlight = 0
	for i := range 3 {
		part, err := Share(rateCfg, i, 3)
		if err != nil {
			t.Fatal(err)
		}
		rateSum += part.Stages[0].From
		toSum += part.Stages[0].To
	}
	if math.Abs(rateSum-10) > 1e-6 || math.Abs(toSum-20) > 1e-6 {
		t.Fatalf("rate from=%v to=%v", rateSum, toSum)
	}
	oneUser := Config{Stages: []Stage{{From: 1, Duration: time.Second}}}
	if _, err := Share(oneUser, 1, 2); err == nil {
		t.Fatal("gave a second part a user that does not exist")
	}
	capped := Config{RPS: 1, Stages: []Stage{{From: 2, Duration: time.Second}}}
	if _, err := Share(capped, 1, 2); err == nil {
		t.Fatal("left a part without a share of the request cap")
	}
}

func TestMergeKeepsTheTail(t *testing.T) {
	fast := hist{}
	fast.add(int64(time.Millisecond))
	slow := hist{}
	slow.add(int64(100 * time.Millisecond))
	part := func(i int, h hist, err string) Snapshot {
		return Snapshot{
			Elapsed: time.Second, Duration: time.Second, Profile: "constant users 2", Thresholds: []string{"p95 < 1ms"},
			Part: Part{Index: i, Of: 2}, VUs: 1, Iterations: 4, Requests: 4, Overall: exportHist(h), Err: err,
		}
	}
	res, err := Merge([]Snapshot{part(0, fast, ""), part(1, slow, "boom")}, []string{"errorRate < 1%"})
	if err == nil || err.Error() != "boom" || res.Passed || res.Iterations != 8 || res.Requests != 8 || res.VUs != 2 {
		t.Fatalf("err=%v %+v", err, res)
	}
	if res.Overall.P95 != 100*time.Millisecond || res.Overall.Mean >= res.Overall.P95 {
		t.Fatalf("merged latency = %+v", res.Overall)
	}
	if len(res.Thresholds) != 2 || res.Thresholds[0].Passed || !res.Thresholds[1].Passed {
		t.Fatalf("thresholds = %+v", res.Thresholds)
	}
	if res.Profile != "constant users 2 across 2 parts" {
		t.Fatalf("profile = %s", res.Profile)
	}
}

func TestMergeRejectsAnIncompleteSet(t *testing.T) {
	part := func(i, of int, profile string) Snapshot {
		return Snapshot{Profile: profile, Duration: time.Second, Part: Part{Index: i, Of: of}}
	}
	for name, snaps := range map[string][]Snapshot{
		"missing":   {part(0, 3, "a"), part(1, 3, "a")},
		"duplicate": {part(0, 2, "a"), part(0, 2, "a")},
		"mixed":     {part(0, 2, "a"), part(1, 2, "b")},
		"empty":     nil,
	} {
		if _, err := Merge(snaps, nil); err == nil {
			t.Errorf("%s: merged", name)
		}
	}
}
