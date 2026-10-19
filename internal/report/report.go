package report

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html/template"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gi-runner/gi/internal/engine"
	"github.com/gi-runner/gi/internal/load"
)

type Document struct {
	Mode     string
	Title    string
	Started  time.Time
	Duration time.Duration
	Features []engine.FileResult
	Load     *load.Result
	Loads    []load.Result
}

func WriteHTML(path string, doc Document) error {
	var buf bytes.Buffer
	if err := htmlTmpl.Execute(&buf, viewOf(doc)); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o600)
}

func WriteJSON(path string, doc Document) error {
	raw, err := json.MarshalIndent(viewOf(doc), "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return os.WriteFile(path, raw, 0o600)
}

func WriteJUnit(path string, doc Document) error {
	suites := junitSuites{Name: "gi"}
	for _, f := range doc.Features {
		suite := junitSuite{Name: f.Name, File: f.Path}
		for _, sc := range f.Scenarios {
			if sc.Skipped {
				suite.Skipped++
				suite.Tests++
				suite.Cases = append(suite.Cases, junitCase{Name: sc.Name, Class: f.Name, Time: seconds(sc.Duration), Skipped: &junitSkip{}})
				continue
			}
			suite.Tests++
			c := junitCase{Name: sc.Name, Class: f.Name, Time: seconds(sc.Duration)}
			if !sc.Passed && !sc.Miss {
				suite.Failures++
				c.Failure = &junitFail{Message: "failed", Body: sc.Error}
			}
			suite.Cases = append(suite.Cases, c)
		}
		suites.Suites = append(suites.Suites, suite)
	}
	for i, ld := range loadsOf(doc) {
		if ld.Passed {
			continue
		}
		suite := junitSuite{Name: fmt.Sprintf("load %d", i+1)}
		for _, th := range ld.Thresholds {
			suite.Tests++
			c := junitCase{Name: th.Text, Class: "load"}
			if !th.Passed {
				suite.Failures++
				c.Failure = &junitFail{Message: "threshold", Body: th.Actual}
			}
			suite.Cases = append(suite.Cases, c)
		}
		if ld.FailedIterations > 0 || suite.Failures == 0 {
			suite.Tests++
			suite.Failures++
			suite.Cases = append(suite.Cases, junitCase{
				Name:    "iterations",
				Class:   "load",
				Failure: &junitFail{Message: "failed iterations", Body: strconv.FormatInt(ld.FailedIterations, 10)},
			})
		}
		suites.Suites = append(suites.Suites, suite)
	}
	raw, err := xml.MarshalIndent(suites, "", "  ")
	if err != nil {
		return err
	}
	raw = append([]byte(xml.Header), raw...)
	raw = append(raw, '\n')
	return os.WriteFile(path, raw, 0o600)
}

func seconds(d time.Duration) string {
	return fmt.Sprintf("%.3f", d.Seconds())
}

type junitSuites struct {
	XMLName xml.Name     `xml:"testsuites"`
	Name    string       `xml:"name,attr"`
	Suites  []junitSuite `xml:"testsuite"`
}

type junitSuite struct {
	XMLName  xml.Name    `xml:"testsuite"`
	Name     string      `xml:"name,attr"`
	File     string      `xml:"file,attr,omitempty"`
	Tests    int         `xml:"tests,attr"`
	Failures int         `xml:"failures,attr"`
	Skipped  int         `xml:"skipped,attr"`
	Cases    []junitCase `xml:"testcase"`
}

type junitCase struct {
	Name    string     `xml:"name,attr"`
	Class   string     `xml:"classname,attr"`
	Time    string     `xml:"time,attr"`
	Failure *junitFail `xml:"failure,omitempty"`
	Skipped *junitSkip `xml:"skipped,omitempty"`
}

type junitFail struct {
	Message string `xml:"message,attr"`
	Body    string `xml:",chardata"`
}

type junitSkip struct{}

type view struct {
	Mode       string     `json:"mode"`
	Title      string     `json:"title"`
	Started    time.Time  `json:"started"`
	DurationMS float64    `json:"durationMs"`
	Passed     int        `json:"passed"`
	Failed     int        `json:"failed"`
	Skipped    int        `json:"skipped"`
	Features   []featView `json:"features"`
	Timeline   []barView  `json:"timeline,omitempty"`
	Load       *loadView  `json:"load,omitempty"`
	Loads      []loadView `json:"loads,omitempty"`
}

type featView struct {
	Path       string   `json:"path"`
	Name       string   `json:"name"`
	DurationMS float64  `json:"durationMs"`
	Scenarios  []scView `json:"scenarios"`
}

type scView struct {
	Name       string     `json:"name"`
	Line       int        `json:"line"`
	Passed     bool       `json:"passed"`
	Skipped    bool       `json:"skipped"`
	DurationMS float64    `json:"durationMs"`
	Error      string     `json:"error,omitempty"`
	Steps      []stepView `json:"steps"`
}

type barView struct {
	Name   string  `json:"name"`
	Passed bool    `json:"passed"`
	Left   float64 `json:"left"`
	Width  float64 `json:"width"`
}

type stepView struct {
	Line       int         `json:"line"`
	Keyword    string      `json:"keyword"`
	Text       string      `json:"text"`
	Passed     bool        `json:"passed"`
	DurationMS float64     `json:"durationMs"`
	Error      string      `json:"error,omitempty"`
	Diff       string      `json:"diff,omitempty"`
	Label      string      `json:"label,omitempty"`
	Output     string      `json:"output,omitempty"`
	HTTP       *httpView   `json:"http,omitempty"`
	Calls      []stepView  `json:"calls,omitempty"`
	Embeds     []embedView `json:"embeds,omitempty"`
}

type embedView struct {
	Name  string       `json:"name,omitempty"`
	Mime  string       `json:"mime"`
	Data  string       `json:"data"`
	Text  string       `json:"-"`
	Image template.URL `json:"-"`
}

type httpView struct {
	Method       string            `json:"method"`
	URL          string            `json:"url"`
	Status       int               `json:"status"`
	DurationMS   float64           `json:"durationMs"`
	Endpoint     string            `json:"endpoint"`
	RequestBody  string            `json:"requestBody,omitempty"`
	ResponseBody string            `json:"responseBody,omitempty"`
	Error        string            `json:"error,omitempty"`
	ReqHeaders   map[string]string `json:"requestHeaders,omitempty"`
	ResHeaders   map[string]string `json:"responseHeaders,omitempty"`
}

type loadView struct {
	VUs              int      `json:"vus"`
	Iterations       int64    `json:"iterations"`
	FailedIterations int64    `json:"failedIterations"`
	Requests         int64    `json:"requests"`
	TransportErrors  int64    `json:"transportErrors"`
	ElapsedMS        float64  `json:"elapsedMs"`
	RPS              float64  `json:"rps"`
	Passed           bool     `json:"passed"`
	P50              string   `json:"p50"`
	P95              string   `json:"p95"`
	P99              string   `json:"p99"`
	Mean             string   `json:"mean"`
	Min              string   `json:"min"`
	Max              string   `json:"max"`
	Endpoints        []epView `json:"endpoints"`
	Thresholds       []thView `json:"thresholds"`
	Failures         []scView `json:"failures,omitempty"`
}

type epView struct {
	Name   string  `json:"name"`
	Count  int64   `json:"count"`
	Errors int64   `json:"errors"`
	RPS    float64 `json:"rps"`
	P50    string  `json:"p50"`
	P95    string  `json:"p95"`
	P99    string  `json:"p99"`
	Mean   string  `json:"mean"`
	Min    string  `json:"min"`
	Max    string  `json:"max"`
}

type thView struct {
	Text   string `json:"text"`
	Passed bool   `json:"passed"`
	Actual string `json:"actual"`
}

func viewOf(doc Document) view {
	v := view{
		Mode:       doc.Mode,
		Title:      doc.Title,
		Started:    doc.Started,
		DurationMS: ms(doc.Duration),
	}
	for _, f := range doc.Features {
		fv := featView{Path: f.Path, Name: f.Name, DurationMS: ms(f.Duration)}
		for _, sc := range f.Scenarios {
			switch {
			case sc.Skipped:
				v.Skipped++
			case sc.Passed:
				v.Passed++
			case !sc.Miss:
				v.Failed++
			}
			fv.Scenarios = append(fv.Scenarios, scenarioView(sc))
		}
		v.Features = append(v.Features, fv)
	}
	v.Timeline = timeline(doc, v.Features)
	for _, ld := range loadsOf(doc) {
		v.Loads = append(v.Loads, loadViewOf(ld))
	}
	if len(v.Loads) > 0 {
		first := v.Loads[0]
		v.Load = &first
	}
	return v
}

func scenarioView(sc engine.ScenarioResult) scView {
	sv := scView{
		Name: sc.Name, Line: sc.Line, Passed: sc.Passed, Skipped: sc.Skipped,
		DurationMS: ms(sc.Duration), Error: sc.Error,
		Steps: stepsView(sc.Steps),
	}
	return sv
}

func stepsView(steps []engine.StepResult) []stepView {
	out := make([]stepView, 0, len(steps))
	for _, st := range steps {
		step := stepView{
			Line: st.Line, Keyword: st.Keyword, Text: st.Text, Passed: st.Passed,
			DurationMS: ms(st.Duration), Error: st.Error, Diff: st.Diff, Label: st.Label, Output: st.Output,
			Calls: stepsView(st.Calls), Embeds: embedsView(st.Embeds),
		}
		if st.HTTP != nil {
			step.HTTP = &httpView{
				Method: st.HTTP.Method, URL: st.HTTP.URL, Status: st.HTTP.Status,
				DurationMS: ms(st.HTTP.Duration), Endpoint: st.HTTP.Endpoint,
				RequestBody: st.HTTP.RequestBody, ResponseBody: st.HTTP.ResponseBody,
				Error: st.HTTP.Error, ReqHeaders: st.HTTP.RequestHeaders, ResHeaders: st.HTTP.ResponseHeaders,
			}
		}
		out = append(out, step)
	}
	return out
}

func timeline(doc Document, features []featView) []barView {
	if doc.Duration <= 0 {
		return nil
	}
	total := doc.Duration.Seconds()
	var bars []barView
	for i, f := range doc.Features {
		if i >= len(features) {
			break
		}
		for _, sc := range f.Scenarios {
			if sc.Skipped || sc.Started.IsZero() {
				continue
			}
			left := sc.Started.Sub(doc.Started).Seconds() / total * 100
			if left < 0 {
				left = 0
			}
			if left > 100 {
				left = 100
			}
			width := sc.Duration.Seconds() / total * 100
			if width < 0.8 {
				width = 0.8
			}
			if left+width > 100 {
				width = 100 - left
			}
			name := features[i].Name + " · " + sc.Name
			bars = append(bars, barView{Name: name, Passed: sc.Passed, Left: left, Width: width})
		}
	}
	if len(bars) == 0 {
		return nil
	}
	return bars
}

func embedsView(in []engine.Embed) []embedView {
	if len(in) == 0 {
		return nil
	}
	out := make([]embedView, len(in))
	for i, item := range in {
		view := embedView{Name: item.Name, Mime: item.Mime, Data: base64.StdEncoding.EncodeToString(item.Data)}
		if imageMIME(item.Mime) {
			// Payload is standard base64 and the type is an image allowlist.
			view.Image = template.URL("data:" + item.Mime + ";base64," + view.Data) //nolint:gosec
		} else {
			view.Text = string(item.Data)
		}
		out[i] = view
	}
	return out
}

func imageMIME(mime string) bool {
	switch mime {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return true
	default:
		return false
	}
}

func ms(d time.Duration) float64 {
	return float64(d) / float64(time.Millisecond)
}

var htmlTmpl = template.Must(template.New("report").Funcs(template.FuncMap{
	"fmtms":   func(v float64) string { return fmt.Sprintf("%.2f ms", v) },
	"fmtrps":  func(v float64) string { return fmt.Sprintf("%.1f", v) },
	"headers": headerBlock,
	"pretty":  prettyBody,
}).Parse(htmlPage))

func headerBlock(m map[string]string) string {
	if len(m) == 0 {
		return ""
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "%s: %s\n", k, m[k])
	}
	return b.String()
}

func loadsOf(doc Document) []load.Result {
	if len(doc.Loads) > 0 {
		return doc.Loads
	}
	if doc.Load != nil {
		return []load.Result{*doc.Load}
	}
	return nil
}

func loadViewOf(ld load.Result) loadView {
	sec := ld.Elapsed.Seconds()
	if sec <= 0 {
		sec = 1
	}
	lv := loadView{
		VUs:              ld.VUs,
		Iterations:       ld.Iterations,
		FailedIterations: ld.FailedIterations,
		Requests:         ld.Requests,
		TransportErrors:  ld.TransportErrors,
		ElapsedMS:        ms(ld.Elapsed),
		RPS:              float64(ld.Requests) / sec,
		Passed:           ld.Passed,
		P50:              ld.Overall.P50.String(),
		P95:              ld.Overall.P95.String(),
		P99:              ld.Overall.P99.String(),
		Mean:             ld.Overall.Mean.String(),
		Min:              ld.Overall.Min.String(),
		Max:              ld.Overall.Max.String(),
	}
	for _, ep := range ld.Endpoints {
		lv.Endpoints = append(lv.Endpoints, epView{
			Name: ep.Name, Count: ep.Count, Errors: ep.Errors, RPS: ep.RPS,
			P50: ep.Stats.P50.String(), P95: ep.Stats.P95.String(), P99: ep.Stats.P99.String(),
			Mean: ep.Stats.Mean.String(), Min: ep.Stats.Min.String(), Max: ep.Stats.Max.String(),
		})
	}
	for _, th := range ld.Thresholds {
		lv.Thresholds = append(lv.Thresholds, thView{Text: th.Text, Passed: th.Passed, Actual: th.Actual})
	}
	for _, sc := range ld.Failures {
		lv.Failures = append(lv.Failures, scenarioView(sc))
	}
	return lv
}

func prettyBody(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	var v any
	if json.Unmarshal([]byte(s), &v) != nil {
		return s
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return s
	}
	return string(b)
}

const htmlPage = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>{{.Title}}</title>
<style>
  :root { color-scheme: dark; }
  body { margin: 0; font: 15px/1.45 ui-sans-serif, system-ui, sans-serif; background: #07080a; color: #eceae4; }
  main { max-width: 980px; margin: 0 auto; padding: 40px 20px 80px; }
  h1 { font-size: 28px; margin: 0 0 6px; letter-spacing: -0.03em; }
  h2 { font-size: 18px; margin: 28px 0 10px; }
  .sub { color: #a39e93; margin-bottom: 22px; }
  .row { display: flex; gap: 12px; flex-wrap: wrap; }
  .card { background: #101218; border: 1px solid #262a33; border-radius: 12px; padding: 14px 16px; min-width: 120px; }
  .card b { display: block; font-size: 22px; }
  .ok { color: #8dcea0; } .bad { color: #f0a3a3; } .dim { color: #a39e93; }
  details { border: 1px solid #262a33; border-radius: 12px; padding: 10px 14px; margin: 8px 0; background: #101218; }
  summary { cursor: pointer; }
  .step { margin: 10px 0 0; padding-top: 8px; border-top: 1px solid #262a33; }
  pre { white-space: pre-wrap; background: #07080a; padding: 10px; border-radius: 8px; overflow: auto; font-size: 12px; }
  table { width: 100%; border-collapse: collapse; }
  th, td { text-align: left; padding: 8px 6px; border-bottom: 1px solid #262a33; font-size: 13px; }
  code { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
  .nest { margin: 4px 0 4px 14px; padding-left: 12px; border-left: 1px solid #262a33; }
  .tl { margin: 8px 0 20px; }
  .tl-row { display: grid; grid-template-columns: 280px 1fr; gap: 10px; align-items: center; margin: 4px 0; }
  .tl-name { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 13px; }
  .tl-track { position: relative; height: 14px; background: #101218; border-radius: 7px; }
  .tl-bar { position: absolute; top: 2px; height: 10px; border-radius: 5px; min-width: 2px; }
  .tl-bar.ok { background: #8dcea0; }
  .tl-bar.bad { background: #f0a3a3; }
</style>
</head>
<body>
<main>
  <h1>{{.Title}}</h1>
  <div class="sub">{{.Mode}} · {{.Started.Format "2006-01-02 15:04:05"}} · {{fmtms .DurationMS}}</div>
  <div class="row">
    <div class="card"><span class="dim">passed</span><b class="ok">{{.Passed}}</b></div>
    <div class="card"><span class="dim">failed</span><b class="bad">{{.Failed}}</b></div>
    <div class="card"><span class="dim">skipped</span><b>{{.Skipped}}</b></div>
  </div>
  {{range .Loads}}
  <h2>Load</h2>
  <div class="row">
    <div class="card"><span class="dim">virtual users</span><b>{{.VUs}}</b></div>
    <div class="card"><span class="dim">iterations</span><b>{{.Iterations}}</b></div>
    <div class="card"><span class="dim">requests</span><b>{{.Requests}}</b></div>
    <div class="card"><span class="dim">req/s</span><b>{{fmtrps .RPS}}</b></div>
    <div class="card"><span class="dim">failed iterations</span><b class="{{if gt .FailedIterations 0}}bad{{else}}ok{{end}}">{{.FailedIterations}}</b></div>
  </div>
  <div class="row" style="margin-top:12px">
    <div class="card"><span class="dim">p50</span><b>{{.P50}}</b></div>
    <div class="card"><span class="dim">p95</span><b>{{.P95}}</b></div>
    <div class="card"><span class="dim">p99</span><b>{{.P99}}</b></div>
    <div class="card"><span class="dim">mean</span><b>{{.Mean}}</b></div>
  </div>
  {{if .Thresholds}}
  <h2>Thresholds</h2>
  <table>
    <tr><th>check</th><th>actual</th><th></th></tr>
    {{range .Thresholds}}<tr><td><code>{{.Text}}</code></td><td>{{.Actual}}</td><td class="{{if .Passed}}ok{{else}}bad{{end}}">{{if .Passed}}pass{{else}}fail{{end}}</td></tr>{{end}}
  </table>
  {{end}}
  {{if .Endpoints}}
  <h2>Endpoints</h2>
  <table>
    <tr><th>endpoint</th><th>count</th><th>req/s</th><th>p50</th><th>p95</th><th>p99</th><th>errors</th></tr>
    {{range .Endpoints}}<tr><td><code>{{.Name}}</code></td><td>{{.Count}}</td><td>{{fmtrps .RPS}}</td><td>{{.P50}}</td><td>{{.P95}}</td><td>{{.P99}}</td><td>{{.Errors}}</td></tr>{{end}}
  </table>
  {{end}}
  {{range .Failures}}
    <details open><summary class="bad">{{.Name}}</summary><pre>{{.Error}}</pre></details>
  {{end}}
  {{end}}
  {{if .Timeline}}
  <h2>Timeline</h2>
  <div class="tl">
    {{range .Timeline}}
    <div class="tl-row">
      <div class="tl-name" title="{{.Name}}">{{.Name}}</div>
      <div class="tl-track"><div class="tl-bar {{if .Passed}}ok{{else}}bad{{end}}" style="left: {{.Left}}%; width: {{.Width}}%"></div></div>
    </div>
    {{end}}
  </div>
  {{end}}
  {{range .Features}}
    <h2>{{.Name}}</h2>
    <div class="dim">{{.Path}}</div>
    {{range .Scenarios}}
      <details {{if not .Passed}}open{{end}}>
        <summary class="{{if .Skipped}}dim{{else if .Passed}}ok{{else}}bad{{end}}">
          {{if .Skipped}}skipped{{else if .Passed}}passed{{else}}failed{{end}}
          · {{.Name}} · {{fmtms .DurationMS}}
        </summary>
        {{if .Error}}<pre>{{.Error}}</pre>{{end}}
        {{template "steps" .Steps}}
      </details>
    {{end}}
  {{end}}
</main>
</body>
</html>
{{define "steps"}}
{{range .}}
  <div class="step">
    <code>{{.Keyword}} {{.Text}}</code>
    <span class="dim">{{fmtms .DurationMS}}</span>
    {{if not .Passed}}<span class="bad">failed</span>{{end}}
    {{if .Output}}<pre>{{.Output}}</pre>{{end}}
    {{range .Embeds}}{{if .Name}}<div class="dim">{{.Name}}</div>{{end}}{{if .Image}}<img alt="{{.Name}}" src="{{.Image}}">{{else}}<pre>{{.Text}}</pre>{{end}}{{end}}
    {{if .Error}}<pre>{{.Error}}</pre>{{end}}
    {{if .Diff}}<pre>{{.Diff}}</pre>{{end}}
    {{if .HTTP}}
      <pre>{{.HTTP.Method}} {{.HTTP.URL}}
status {{.HTTP.Status}} · {{fmtms .HTTP.DurationMS}}{{if .HTTP.Error}}
{{.HTTP.Error}}{{end}}{{if .HTTP.ReqHeaders}}
{{headers .HTTP.ReqHeaders}}{{end}}{{if .HTTP.RequestBody}}
{{pretty .HTTP.RequestBody}}
{{end}}{{if .HTTP.ResHeaders}}
{{headers .HTTP.ResHeaders}}{{end}}{{if .HTTP.ResponseBody}}
{{pretty .HTTP.ResponseBody}}{{end}}</pre>
    {{end}}
    {{if .Calls}}<div class="nest">{{template "steps" .Calls}}</div>{{end}}
  </div>
{{end}}
{{end}}
`
