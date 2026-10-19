package engine_test

import (
	"path/filepath"
	"strconv"
	"testing"

	"github.com/gi-runner/gi/internal/engine"
	"github.com/gi-runner/gi/internal/feature"
)

func TestPinnedUpstreamFeatures(t *testing.T) {
	for _, name := range []string{"copy/copy.feature", "xml/xml-call.feature"} {
		for _, jobs := range []int{1, 4} {
			t.Run(name+"/"+strconv.Itoa(jobs), func(t *testing.T) {
				path := filepath.Join("..", "..", "testdata", "karate", filepath.FromSlash(name))
				f, err := feature.ParseFile(path)
				if err != nil {
					t.Fatal(err)
				}
				result, err := engine.RunFile(t.Context(), f, engine.Options{Jobs: jobs})
				if err != nil {
					t.Fatal(err)
				}
				if !result.Passed() {
					for _, sc := range result.Scenarios {
						if !sc.Passed {
							t.Error(sc.Name + ": " + sc.Error)
						}
					}
				}
			})
		}
	}
}
