package match_test

import (
	"testing"

	"github.com/gi-runner/gi/internal/match"
)

func BenchmarkMatchEachNumbers(b *testing.B) {
	values := make([]any, 1000)
	for i := range values {
		values[i] = int64(i)
	}
	b.ReportAllocs()
	for b.Loop() {
		if result := match.MatchEach(values, "#number", match.Equals, match.Context{}); !result.Pass {
			b.Fatal(result.Message)
		}
	}
}

func BenchmarkUnorderedAmbiguous(b *testing.B) {
	actual := make([]any, 100)
	expected := make([]any, 100)
	for i := range actual {
		actual[i] = int64(i)
		expected[i] = "#number"
	}
	b.ReportAllocs()
	for b.Loop() {
		if result := match.Match(actual, expected, match.ContainsOnly, match.Context{}); !result.Pass {
			b.Fatal(result.Message)
		}
	}
}

func BenchmarkUnorderedDistinct(b *testing.B) {
	actual := make([]any, 100)
	expected := make([]any, 100)
	for i := range actual {
		actual[i] = int64(i)
		expected[len(actual)-i-1] = int64(i)
	}
	b.ReportAllocs()
	for b.Loop() {
		if result := match.Match(actual, expected, match.ContainsOnly, match.Context{}); !result.Pass {
			b.Fatal(result.Message)
		}
	}
}
