package report

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gi-runner/gi/internal/engine"
)

func ExportOTLP(ctx context.Context, endpoint string, doc Document) error {
	if endpoint == "" {
		return nil
	}
	payload := map[string]any{"resourceSpans": []any{map[string]any{
		"resource": map[string]any{"attributes": []any{attr("service.name", "gi")}},
		"scopeSpans": []any{map[string]any{
			"scope": map[string]any{"name": "gi"},
			"spans": spansOf(doc),
		}},
	}}}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("otel collector returned %s", resp.Status)
	}
	return nil
}

func spansOf(doc Document) []any {
	var spans []any
	for _, f := range doc.Features {
		for _, sc := range f.Scenarios {
			if sc.Skipped {
				continue
			}
			trace := hexID(16)
			parent := hexID(8)
			start := sc.Started
			if start.IsZero() {
				start = doc.Started
			}
			end := start.Add(sc.Duration)
			status := 1
			if !sc.Passed {
				status = 2
			}
			spans = append(spans, span(trace, parent, "", sc.Name, start, end, status, []any{
				attr("gi.feature", f.Name),
			}))
			spans = append(spans, httpSpans(trace, parent, sc.Steps, start)...)
		}
	}
	return spans
}

func httpSpans(trace, parent string, steps []engine.StepResult, start time.Time) []any {
	var spans []any
	for _, st := range steps {
		if st.HTTP != nil {
			hs := st.HTTP.Started
			if hs.IsZero() {
				hs = st.Started
			}
			if hs.IsZero() {
				hs = start
			}
			he := hs.Add(st.HTTP.Duration)
			code := 1
			if st.HTTP.Error != "" {
				code = 2
			}
			spans = append(spans, span(trace, hexID(8), parent, st.HTTP.Endpoint, hs, he, code, []any{
				attr("http.request.method", st.HTTP.Method),
				attr("url.full", st.HTTP.URL),
				attrInt("http.response.status_code", st.HTTP.Status),
			}))
		}
		if len(st.Calls) > 0 {
			spans = append(spans, httpSpans(trace, parent, st.Calls, start)...)
		}
	}
	return spans
}

func span(trace, id, parent, name string, start, end time.Time, status int, attrs []any) map[string]any {
	s := map[string]any{
		"traceId":           trace,
		"spanId":            id,
		"name":              name,
		"kind":              3,
		"startTimeUnixNano": strconv.FormatInt(start.UnixNano(), 10),
		"endTimeUnixNano":   strconv.FormatInt(end.UnixNano(), 10),
		"attributes":        attrs,
		"status":            map[string]any{"code": status},
	}
	if parent != "" {
		s["parentSpanId"] = parent
	}
	return s
}

func attr(k, v string) map[string]any {
	return map[string]any{"key": k, "value": map[string]any{"stringValue": v}}
}

func attrInt(k string, v int) map[string]any {
	return map[string]any{"key": k, "value": map[string]any{"intValue": strconv.Itoa(v)}}
}

func hexID(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
