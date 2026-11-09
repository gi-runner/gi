package load

import (
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gi-runner/gi/internal/engine"
	"github.com/gi-runner/gi/internal/feature"
)

const mixed = `Feature: shop
@setup
Scenario: seed
* def token = 't'
@weight=3
Scenario: browse
* url 'http://shop.test/browse'
* method get
@weight=1
Scenario: buy
* url 'http://shop.test/buy'
* method post
@noload
Scenario: audit
* url 'http://shop.test/audit'
* method get
Scenario: unweighted
* url 'http://shop.test/other'
* method get
`

func TestMixInterleavesByWeight(t *testing.T) {
	f, err := feature.Parse("shop.feature", mixed)
	if err != nil {
		t.Fatal(err)
	}
	m, err := newMix(f, engine.Options{LoadMode: true})
	if err != nil || m == nil || len(m.files) != 3 || m.total != 5 {
		t.Fatalf("mix = %+v %v", m, err)
	}
	order := make([]string, 0, 10)
	for range 10 {
		file := m.next()
		if len(file.Scenarios) != 2 || !file.Scenarios[0].IsSetup {
			t.Fatalf("setup missing from %+v", file.Scenarios)
		}
		order = append(order, file.Scenarios[1].Name)
	}
	got := strings.Join(order, ",")
	want := "browse,buy,browse,unweighted,browse,browse,buy,browse,unweighted,browse"
	if got != want {
		t.Fatalf("order = %s", got)
	}
	plain, err := feature.Parse("plain.feature", "Feature: p\nScenario: a\n* assert true\n")
	if err != nil {
		t.Fatal(err)
	}
	if m, err := newMix(plain, engine.Options{LoadMode: true}); m != nil || err != nil {
		t.Fatalf("unweighted file mixed: %+v %v", m, err)
	}
	bad, err := feature.Parse("bad.feature", "Feature: b\n@weight=x\nScenario: a\n* assert true\n")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := newMix(bad, engine.Options{LoadMode: true}); err == nil {
		t.Fatal("accepted @weight=x")
	}
}

func TestRunHonorsTheMixAndReportsProgress(t *testing.T) {
	f, err := feature.Parse("shop.feature", mixed)
	if err != nil {
		t.Fatal(err)
	}
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: http.NoBody, Request: r}, nil
	})
	var ticks atomic.Int64
	res, err := Run(t.Context(), f, Config{
		VUs: 2, Duration: 1200 * time.Millisecond,
		Client: &http.Client{Transport: transport},
		Progress: func(p Progress) {
			if p.Total <= 0 || p.Elapsed <= 0 {
				t.Errorf("progress = %+v", p)
			}
			ticks.Add(1)
		},
	})
	if err != nil || !res.Passed {
		t.Fatalf("%v %+v", err, res)
	}
	count := map[string]int64{}
	for _, ep := range res.Endpoints {
		count[ep.Name[strings.LastIndex(ep.Name, "/")+1:]] = ep.Count
	}
	if count["audit"] != 0 || count["buy"] == 0 {
		t.Fatalf("endpoints = %v", count)
	}
	if ratio := float64(count["browse"]) / float64(count["buy"]); ratio < 2.9 || ratio > 3.1 {
		t.Fatalf("browse:buy = %.2f, counts %v", ratio, count)
	}
	if ticks.Load() == 0 {
		t.Fatal("no progress reported")
	}
}
