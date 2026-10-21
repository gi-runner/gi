package load

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gi-runner/gi/internal/feature"
)

func TestFatalConfigurationStopsScheduledArrivals(t *testing.T) {
	f, err := feature.Parse("fatal.feature", "Feature: fatal\nScenario: never runs\n* def ok = true\n")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	result, err := Run(ctx, f, Config{Rate: 1000, Duration: time.Hour, Tag: "("})
	if err == nil || errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "tag:") {
		t.Fatalf("did not fail promptly with the configuration error: %v", err)
	}
	if result.Passed || result.Requests != 0 {
		t.Fatalf("invalid run passed or made requests: %+v", result)
	}
}
