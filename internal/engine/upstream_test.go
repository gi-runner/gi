package engine_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/gi-runner/gi/internal/engine"
	"github.com/gi-runner/gi/internal/feature"
)

func TestPinnedUpstreamFeatures(t *testing.T) {
	for _, name := range []string{"copy/copy.feature", "xml/xml-call.feature", "config/config.feature"} {
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

func TestUpstreamFixtureManifest(t *testing.T) {
	root := filepath.Join("..", "..", "testdata", "karate")
	raw, err := os.ReadFile(filepath.Join(root, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Commit string `json:"commit"`
		Files  map[string]struct {
			Source string `json:"source"`
			SHA256 string `json:"sha256"`
		} `json:"files"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Commit) != 40 || len(manifest.Files) == 0 {
		t.Fatal("missing upstream commit or fixture inventory")
	}
	for name, entry := range manifest.Files {
		raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			t.Error(err)
			continue
		}
		sum := sha256.Sum256(bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n")))
		if hex.EncodeToString(sum[:]) != entry.SHA256 {
			t.Errorf("fixture %s differs from pinned upstream contents", name)
		}
		if entry.Source == "" {
			t.Errorf("fixture %s has no upstream path", name)
		}
	}
}
