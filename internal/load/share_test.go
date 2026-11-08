package load

import (
	"math"
	"testing"
	"time"
)

func TestShareSplitsUsersRateAndCaps(t *testing.T) {
	cfg := Config{
		Injectors:   3,
		RPS:         10,
		MaxInFlight: 4,
		Duration:    time.Second,
		Stages: []Stage{
			{From: 10, To: 40, ToSet: true, Duration: time.Second},
			{Rate: true, From: 10, To: 20, ToSet: true, Duration: time.Second},
		},
		Thresholds: []string{"p95 < 1s"},
	}
	cfg.Stages = cfg.Stages[:1]
	var users int
	var rateSum, toSum float64
	for i := range 3 {
		part, err := Share(cfg, i, 3)
		if err != nil {
			t.Fatal(err)
		}
		if part.Injectors != 1 || part.Thresholds != nil || part.RPS < 1 {
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
		t.Fatal("gave a second injector a user that does not exist")
	}
	capped := Config{RPS: 1, Stages: []Stage{{From: 2, Duration: time.Second}}}
	if _, err := Share(capped, 1, 2); err == nil {
		t.Fatal("left an injector without a share of the request cap")
	}
}

func TestMergeKeepsTheTail(t *testing.T) {
	fast := hist{}
	fast.add(int64(time.Millisecond))
	slow := hist{}
	slow.add(int64(100 * time.Millisecond))
	cfg := Config{
		Duration:     time.Second,
		MaxEndpoints: 10,
		Injectors:    2,
		Thresholds:   []string{"p95 < 1ms"},
		Stages:       []Stage{{From: 2, Duration: time.Second}},
	}
	res, err := Merge([]Snapshot{
		{Elapsed: time.Second, VUs: 1, Iterations: 4, Requests: 4, Overall: exportHist(fast)},
		{Elapsed: time.Second, VUs: 1, Iterations: 4, Requests: 4, Overall: exportHist(slow), Err: "boom"},
	}, cfg)
	if err == nil || err.Error() != "boom" || res.Passed || res.Iterations != 8 || res.Requests != 8 || res.VUs != 2 {
		t.Fatalf("err=%v %+v", err, res)
	}
	if res.Overall.P95 != 100*time.Millisecond || res.Overall.Mean >= res.Overall.P95 {
		t.Fatalf("merged latency = %+v", res.Overall)
	}
	if len(res.Thresholds) != 1 || res.Thresholds[0].Passed {
		t.Fatalf("thresholds = %+v", res.Thresholds)
	}
	if res.Profile != "constant users 2 across 2 injectors" {
		t.Fatalf("profile = %s", res.Profile)
	}
}
