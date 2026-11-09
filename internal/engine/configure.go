package engine

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/gi-runner/gi/internal/value"
)

// deprecated configure keys are ignored with a hint, as in Karate v2.
var deprecated = map[string]string{
	"logPrettyRequest":         "use configure logging = { pretty: true }",
	"logPrettyResponse":        "use configure logging = { pretty: true }",
	"printEnabled":             "use configure logging = { console: 'warn' } to silence print",
	"lowerCaseResponseHeaders": "match header is case-insensitive; use karate.lowerCase(responseHeaders)",
	"logModifier":              "use configure logging = { mask: { headers: [...], patterns: [...] } }",
}

var flags = map[string]func(*session) *bool{
	"matchEachEmptyAllowed":  func(s *session) *bool { return &s.matchEachEmpty },
	"strictResponseParsing":  func(s *session) *bool { return &s.strictParsing },
	"followRedirects":        func(s *session) *bool { return &s.follow },
	"continueOnStepFailure":  func(s *session) *bool { return &s.continueOnFail },
	"cors":                   func(s *session) *bool { return &s.cors },
	"httpRetryEnabled":       func(s *session) *bool { return &s.httpRetry },
	"abortSuiteOnFailure":    func(s *session) *bool { return &s.abortSuite },
	"abortedStepsShouldPass": func(s *session) *bool { return &s.abortedPass },
}

func (e *exec) configure(rest string) error {
	name, expr, ok := splitAssign(rest)
	if !ok {
		return errors.New("configure expects name = value")
	}
	name = strings.TrimSpace(name)
	expr = strings.TrimSpace(expr)
	switch name {
	case "headers":
		return e.vm.ConfigureHeaders(expr)
	case "beforeScenario", "afterScenario", "afterFeature", "afterScenarioOutline", "onStepFailure":
		return e.vm.ConfigureHook(name, expr)
	}
	val, err := e.eval(expr)
	if err != nil {
		return err
	}
	return e.apply(name, val)
}

func (e *exec) apply(name string, val any) error {
	if hint, ok := deprecated[name]; ok {
		e.opt.Suite.Warn(os.Stderr, name, fmt.Sprintf("configure %s is ignored; %s", name, hint))
		return nil
	}
	if field := flags[name]; field != nil {
		b, ok := val.(bool)
		if !ok {
			return fmt.Errorf("%s expects true or false", name)
		}
		*field(&e.sess) = b
		return nil
	}
	switch name {
	case "timeout", "readTimeout":
		n, ok := value.AsInt(val)
		if !ok || n <= 0 || int64(n) > math.MaxInt64/int64(time.Millisecond) {
			return errors.New("timeout expects positive milliseconds within the duration range")
		}
		e.sess.timeout = time.Duration(n) * time.Millisecond
	case "url":
		e.sess.baseURL = value.Sprint(val)
	case "ssl":
		return e.configureSSL(val)
	case "proxy":
		return e.configureProxy(val)
	case "connectTimeout":
		return e.configureConnectTimeout(val)
	case "localAddress":
		ip := ""
		if val != nil {
			ip = value.Sprint(val)
			if net.ParseIP(ip) == nil {
				return errors.New("localAddress expects an IP address")
			}
		}
		e.sess.transport.LocalAddr = ip
	case "retry":
		return e.vm.ConfigureRetry(val)
	case "auth":
		return e.setAuth(val)
	case "cookies":
		return e.configureCookies(val)
	case "charset":
		s, ok := val.(string)
		if val != nil && (!ok || s == "") {
			return errors.New("charset expects a name such as 'utf-8', or null")
		}
		e.sess.charset = s
	case "responseHeaders":
		m, ok := value.AsMap(val)
		if !ok && val != nil {
			return errors.New("responseHeaders expects an object")
		}
		e.sess.mockHeaders = m
	case "callSingleCache":
		return e.configureSingleCache(val)
	case "report":
		return e.sess.out.report(val)
	case "logging":
		return e.sess.out.logging(val)
	case "driver":
		return e.configureDriver(val)
	default:
		return fmt.Errorf("unknown configure %q", name)
	}
	return nil
}

func (e *exec) configureCookies(val any) error {
	if val == nil {
		e.sess.cookies = nil
		return nil
	}
	m, ok := value.AsMap(val)
	if !ok {
		return errors.New("cookies expects an object of names to values")
	}
	cookies := make([][2]string, 0, len(m))
	for name, v := range m {
		if c, ok := value.AsMap(v); ok {
			v = c["value"]
		}
		cookies = append(cookies, [2]string{name, value.Sprint(v)})
	}
	slices.SortFunc(cookies, func(a, b [2]string) int { return strings.Compare(a[0], b[0]) })
	e.sess.cookies = cookies
	return nil
}

func (e *exec) configureSingleCache(val any) error {
	m, ok := value.AsMap(val)
	if !ok {
		return errors.New("callSingleCache expects { minutes, dir }")
	}
	next := e.sess.singleCache
	if raw, ok := m["minutes"]; ok {
		n, ok := value.AsInt(raw)
		if !ok || n < 0 {
			return errors.New("callSingleCache minutes expects a nonnegative integer")
		}
		next.minutes = n
	}
	if raw, ok := m["dir"]; ok && raw != nil {
		next.dir = value.Sprint(raw)
	}
	e.sess.singleCache = next
	return nil
}

// DriverConfig is the browser launch setting from configure driver.
type DriverConfig struct {
	Headless    bool
	Executable  string
	UserDataDir string
	Args        []string
}

// Process settings that a CDP launch does not need.
var driverIgnored = []string{"start", "port", "host", "showDriverLog", "showProcessLog", "showBrowserLog", "timeout", "pollAttempts", "pollInterval"}

func (e *exec) configureDriver(val any) error {
	m, ok := value.AsMap(val)
	if !ok {
		return errors.New("driver expects an object")
	}
	cfg := DriverConfig{Headless: true}
	for key, v := range m {
		switch key {
		case "type":
			if t := value.Sprint(v); t != "chrome" && t != "chromedriver" {
				return fmt.Errorf("driver type %q is not available; gi drives Chrome", t)
			}
		case "headless":
			b, ok := v.(bool)
			if !ok {
				return errors.New("driver headless expects true or false")
			}
			cfg.Headless = b
		case "executable":
			cfg.Executable = value.Sprint(v)
		case "userDataDir":
			cfg.UserDataDir = value.Sprint(v)
		case "addOptions":
			list, ok := value.AsSlice(v)
			if !ok {
				return errors.New("driver addOptions expects a list")
			}
			for _, item := range list {
				cfg.Args = append(cfg.Args, value.Sprint(item))
			}
		default:
			if !slices.Contains(driverIgnored, key) {
				return fmt.Errorf("unsupported driver option %q", key)
			}
		}
	}
	e.sess.driver = cfg
	return nil
}

// output shapes what a scenario writes to the console and the report.
type output struct {
	pretty, quiet  bool
	hideLog, terse bool
	unmasked       bool
	maskHeaders    []string
	maskPatterns   []*regexp.Regexp
}

func (o *output) report(val any) error {
	if b, ok := val.(bool); ok {
		o.hideLog, o.terse = !b, !b
		return nil
	}
	m, ok := value.AsMap(val)
	if !ok {
		return errors.New("report expects true, false, or { showLog, showAllSteps }")
	}
	for key, v := range m {
		b, ok := v.(bool)
		switch {
		case key != "showLog" && key != "showAllSteps":
			return fmt.Errorf("unsupported report option %q", key)
		case !ok:
			return fmt.Errorf("report %s expects true or false", key)
		case key == "showLog":
			o.hideLog = !b
		default:
			o.terse = !b
		}
	}
	return nil
}

var levels = map[string]int{"trace": 0, "debug": 1, "info": 2, "warn": 3, "error": 4, "off": 5}

func level(v any) (int, error) {
	n, ok := levels[strings.ToLower(value.Sprint(v))]
	if !ok {
		return 0, fmt.Errorf("unknown log level %q", value.Sprint(v))
	}
	return n, nil
}

func (o *output) logging(val any) error {
	m, ok := value.AsMap(val)
	if !ok {
		return errors.New("logging expects an object")
	}
	for key, v := range m {
		switch key {
		case "console", "report":
			n, err := level(v)
			if err != nil {
				return err
			}
			if key == "console" {
				o.quiet = n > levels["info"]
			} else {
				o.hideLog = n > levels["info"]
			}
		case "pretty":
			b, ok := v.(bool)
			if !ok {
				return errors.New("logging pretty expects true or false")
			}
			o.pretty = b
		case "mask":
			if err := o.mask(v); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported logging option %q", key)
		}
	}
	return nil
}

func (o *output) mask(v any) error {
	o.unmasked, o.maskHeaders, o.maskPatterns = false, nil, nil
	switch v {
	case nil, true:
		return nil
	case false:
		o.unmasked = true
		return nil
	}
	m, ok := value.AsMap(v)
	if !ok {
		return errors.New("logging mask expects true, false, or { headers, patterns }")
	}
	for key, raw := range m {
		list, ok := value.AsSlice(raw)
		if !ok {
			return fmt.Errorf("logging mask %s expects a list", key)
		}
		for _, item := range list {
			s := value.Sprint(item)
			switch key {
			case "headers":
				o.maskHeaders = append(o.maskHeaders, strings.ToLower(s))
			case "patterns":
				re, err := regexp.Compile(s)
				if err != nil {
					return fmt.Errorf("logging mask pattern: %w", err)
				}
				o.maskPatterns = append(o.maskPatterns, re)
			default:
				return fmt.Errorf("unsupported logging mask option %q; use headers or patterns", key)
			}
		}
	}
	return nil
}

// headers flattens headers for the report, masking secrets.
func (o *output) headers(h map[string][]string) map[string]string {
	out := make(map[string]string, len(h))
	for k, vs := range h {
		joined := strings.Join(vs, ", ")
		if !o.unmasked && (secretHeader(k) || slices.Contains(o.maskHeaders, strings.ToLower(k))) {
			joined = "***"
		}
		out[k] = joined
	}
	return out
}

// body prepares a request or response body for the report.
func (o *output) body(b []byte) string {
	text := string(b)
	var indented bytes.Buffer
	if o.pretty && json.Indent(&indented, b, "", "  ") == nil {
		text = indented.String()
	}
	for _, re := range o.maskPatterns {
		text = re.ReplaceAllString(text, "***")
	}
	return value.Truncate(text, 32<<10)
}

// shape trims a scenario's report as configure report and logging ask.
func (o *output) shape(res *ScenarioResult) {
	if o.hideLog || o.terse {
		res.Steps = o.steps(res.Steps)
	}
}

func (o *output) steps(steps []StepResult) []StepResult {
	out := steps[:0]
	for _, st := range steps {
		if o.hideLog {
			st.Output = ""
		}
		st.Calls = o.steps(st.Calls)
		if o.terse && st.Passed && st.HTTP == nil && len(st.Calls) == 0 && len(st.Embeds) == 0 {
			continue
		}
		out = append(out, st)
	}
	return out
}
