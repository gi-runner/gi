// Package spec compiles a Spec Kit, Kiro, or OpenSpec directory into gi features.
// A scenario becomes a request only when the spec or its OpenAPI contract names one call.
package spec

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// File is one feature written under the spec's features directory.
type File struct {
	Path      string   `json:"path"`
	Scenarios int      `json:"scenarios"`
	Load      bool     `json:"load"`
	Clarifies []string `json:"clarifies,omitempty"`
}

// Note is a scenario, edge, or requirement the compiler did not turn into a request.
type Note struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

// Result is what Compile wrote and what it refused to guess.
type Result struct {
	Files []File `json:"files"`
	Notes []Note `json:"notes"`
}

// Compile reads path and writes feature files. out overrides the features directory
// when path is a single spec. An empty out uses <spec>/features.
func Compile(path, out string) (Result, error) {
	dirs, err := specDirs(path)
	if err != nil {
		return Result{}, err
	}
	if out != "" && len(dirs) != 1 {
		return Result{}, fmt.Errorf("--out applies to one spec directory, found %d", len(dirs))
	}
	result := Result{Files: []File{}, Notes: []Note{}}
	for _, dir := range dirs {
		one, err := compileDir(dir, out)
		if err != nil {
			return Result{}, err
		}
		result.Files = append(result.Files, one.Files...)
		result.Notes = append(result.Notes, one.Notes...)
	}
	return result, nil
}

func compileDir(dir, out string) (Result, error) {
	docPath, err := docFile(dir)
	if err != nil {
		return Result{}, err
	}
	text, err := os.ReadFile(docPath)
	if err != nil {
		return Result{}, err
	}
	doc := parseDoc(string(text))
	source := filepath.Base(docPath)
	for i := range doc.stories {
		for j := range doc.stories[i].scenes {
			doc.stories[i].scenes[j].file = source
		}
	}
	ops, err := readContracts(dir)
	if err != nil {
		return Result{}, err
	}
	built := build(doc, ops)
	dest := filepath.Join(dir, "features")
	if out != "" {
		dest = out
	}
	if err := os.MkdirAll(dest, 0o750); err != nil {
		return Result{}, err
	}
	if err := cleanOwned(dest); err != nil {
		return Result{}, err
	}
	var result Result
	result.Notes = built.notes
	for _, f := range built.files {
		path := filepath.Join(dest, f.slug+".feature")
		if err := os.WriteFile(path, []byte(f.text), 0o600); err != nil {
			return Result{}, err
		}
		result.Files = append(result.Files, File{Path: path, Scenarios: f.count, Load: f.load, Clarifies: f.clarifies})
	}
	return result, nil
}

func specDirs(path string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		dir := filepath.Dir(path)
		if _, err := docFile(dir); err != nil {
			return nil, fmt.Errorf("%s is not a spec.md or requirements.md", path)
		}
		return []string{dir}, nil
	}
	if _, err := docFile(path); err == nil {
		return []string{path}, nil
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	var dirs []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(path, entry.Name())
		if _, err := docFile(dir); err == nil {
			dirs = append(dirs, dir)
		}
	}
	if len(dirs) == 0 {
		return nil, fmt.Errorf("no spec.md or requirements.md in %s", path)
	}
	return dirs, nil
}

func docFile(dir string) (string, error) {
	for _, name := range []string{"spec.md", "requirements.md"} {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("no spec.md or requirements.md in %s", dir)
}

func cleanOwned(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".feature") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		text, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		line, _, _ := strings.Cut(string(text), "\n")
		if strings.HasPrefix(strings.TrimSpace(line), "# gi-spec:") {
			if err := os.Remove(path); err != nil {
				return err
			}
		}
	}
	return nil
}
