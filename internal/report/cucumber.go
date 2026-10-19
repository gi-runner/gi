package report

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"strings"

	"github.com/gi-runner/gi/internal/engine"
)

func WriteCucumber(path string, doc Document) error {
	raw, err := json.MarshalIndent(cucumberOf(doc), "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return os.WriteFile(path, raw, 0o600)
}

func cucumberOf(doc Document) []map[string]any {
	out := make([]map[string]any, 0, len(doc.Features))
	for _, f := range doc.Features {
		elements := make([]any, 0, len(f.Scenarios))
		for _, sc := range f.Scenarios {
			steps := make([]any, 0, len(sc.Steps))
			hasFailure := false
			for _, st := range sc.Steps {
				keyword := st.Keyword
				if keyword != "" && !strings.HasSuffix(keyword, " ") {
					keyword += " "
				}
				status := stepStatus(st)
				if sc.Skipped || sc.Miss {
					status = "skipped"
				} else if sc.Passed {
					status = "passed"
				}
				hasFailure = hasFailure || status == "failed"
				result := map[string]any{"status": status, "duration": st.Duration.Nanoseconds()}
				if st.Error != "" && status == "failed" {
					result["error_message"] = st.Error
				}
				step := map[string]any{
					"keyword": keyword,
					"name":    st.Text,
					"line":    st.Line,
					"result":  result,
				}
				if len(st.Embeds) > 0 {
					embeddings := make([]any, len(st.Embeds))
					for i, item := range st.Embeds {
						embeddings[i] = map[string]any{
							"data":      base64.StdEncoding.EncodeToString(item.Data),
							"mime_type": item.Mime,
							"name":      item.Name,
						}
					}
					step["embeddings"] = embeddings
				}
				steps = append(steps, step)
			}
			// Some failures (for example @fail passing unexpectedly) belong to
			// the scenario, so represent them as a Cucumber after-hook result.
			after := []any{}
			if !sc.Passed && !sc.Skipped && !sc.Miss && !hasFailure {
				message := sc.Error
				if message == "" {
					message = "scenario failed"
				}
				after = append(after, map[string]any{"match": map[string]any{"location": "gi:scenario"}, "result": map[string]any{"status": "failed", "duration": 0, "error_message": message}})
			}
			elements = append(elements, map[string]any{
				"keyword": "Scenario",
				"type":    "scenario",
				"name":    sc.Name,
				"line":    sc.Line,
				"tags":    cucumberTags(sc.Tags),
				"steps":   steps,
				"after":   after,
			})
		}
		out = append(out, map[string]any{
			"uri":      f.Path,
			"id":       f.Name,
			"keyword":  "Feature",
			"name":     f.Name,
			"line":     1,
			"elements": elements,
		})
	}
	return out
}

func stepStatus(st engine.StepResult) string {
	if st.Passed {
		return "passed"
	}
	return "failed"
}

func cucumberTags(tags []string) []any {
	out := make([]any, 0, len(tags))
	for _, tag := range tags {
		if tag != "" && !strings.HasPrefix(tag, "@") {
			tag = "@" + tag
		}
		out = append(out, map[string]any{"name": tag})
	}
	return out
}
