package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/gi-runner/gi/internal/value"
	"github.com/gi-runner/gi/internal/xml"
)

var uuidPath = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)
var numPath = regexp.MustCompile(`/\d+`)

func endpointName(method, raw string) string {
	u, err := url.Parse(raw)
	p := raw
	if err == nil {
		p = u.Path
	}
	p = uuidPath.ReplaceAllString(p, "{id}")
	p = numPath.ReplaceAllString(p, "/{id}")
	return strings.ToUpper(method) + " " + p
}

func callFile(e *exec) string {
	if e.file == nil || e.file.Path == "" {
		return ""
	}
	return filepath.Base(e.file.Path)
}

func callName(file string, line int, method, raw string) string {
	name := endpointName(method, raw)
	if file == "" && line <= 0 {
		return name
	}
	if line > 0 {
		return fmt.Sprintf("%s:%d %s", file, line, name)
	}
	return file + " " + name
}

func (e *exec) method(ctx context.Context, name string, sr *StepResult) error {
	method, err := e.methodName(name)
	if err != nil {
		return err
	}
	if e.sess.baseURL == "" {
		return errors.New("url is not set")
	}
	if e.opt.HTTP == nil {
		return errors.New("http sender is not set")
	}
	full, err := buildURL(e.sess.baseURL, e.sess.paths, e.sess.params)
	if err != nil {
		return err
	}
	if (method == "GET" || method == "HEAD") && len(e.sess.form) > 0 {
		if full, err = addQuery(full, e.sess.form); err != nil {
			return err
		}
	}
	body, ctype, err := e.takeBody(method)
	if err != nil {
		return err
	}
	if err := e.ensureToken(ctx, full); err != nil {
		return err
	}
	until, attempts, every := e.vm.TakeRetryUntil()
	if until == "" && e.sess.httpRetry {
		attempts, every = e.vm.RetryPolicy()
	}
	var last error
	for i := range attempts {
		if i > 0 {
			if err := sleep(ctx, every); err != nil {
				return err
			}
		}
		if e.opt.BeforeRequest != nil {
			if err := e.opt.BeforeRequest(ctx); err != nil {
				return err
			}
		}
		headers, err := e.requestHeaders(ctype, len(body) > 0)
		if err != nil {
			return err
		}
		reqCtx, cancel := context.WithTimeout(ctx, e.sess.timeout)
		requestStart := time.Now()
		reply, err := e.opt.HTTP.Send(reqCtx, Call{
			Method: method, URL: full, Header: headers, Cookies: e.sess.cookies, Jar: e.sess.jar,
			Body: body, Follow: e.sess.follow, Transport: e.sess.transport,
		})
		cancel()
		e.sess.prev = map[string]any{"method": method, "url": full, "headers": headerAny(headers), "body": body}
		exch := &Exchange{
			Started: requestStart, Method: method, URL: full, Status: reply.Status, Duration: reply.Duration,
			RequestHeaders: e.sess.out.headers(headers), RequestBody: e.sess.out.body(body),
			Endpoint: callName(callFile(e), sr.Line, method, full),
		}
		sr.HTTP = exch
		if err != nil {
			exch.Error = err.Error()
			e.observe(exch)
			last = fmt.Errorf("request failed: %w", err)
			continue
		}
		exch.ResponseHeaders = e.sess.out.headers(reply.Header)
		exch.ResponseBody = e.sess.out.body(reply.Body)
		e.observe(exch)
		if err := e.keepResponse(reply, full); err != nil {
			return err
		}
		if until == "" {
			return nil
		}
		ok, err := e.vm.Truthy(until, nil, nil)
		if err != nil || ok {
			return err
		}
		last = fmt.Errorf("retry until not satisfied: %s", until)
	}
	if last == nil {
		return fmt.Errorf("retry until not satisfied: %s", until)
	}
	return last
}

func (e *exec) methodName(name string) (string, error) {
	method := strings.ToUpper(strings.TrimSpace(name))
	switch method {
	case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS":
		return method, nil
	}
	v, err := e.eval(name)
	if err != nil {
		return "", fmt.Errorf("unknown method %q: %w", name, err)
	}
	method = strings.ToUpper(value.Sprint(v))
	if method == "" || strings.ContainsAny(method, " \t\r\n") {
		return "", fmt.Errorf("invalid method %q", method)
	}
	return method, nil
}

// takeBody encodes the pending body, parts, or form and clears the request.
func (e *exec) takeBody(method string) (body []byte, ctype string, err error) {
	s := &e.sess
	sends := method != "GET" && method != "HEAD"
	switch {
	case !sends:
	case len(s.parts) > 0:
		body, ctype, err = encodeParts(s.parts)
	case s.hasBody:
		switch b := s.body.(type) {
		case *xml.Document:
			body, ctype = []byte(b.String()), "application/xml"
		case []byte:
			body, ctype = b, "application/octet-stream"
		case string:
			body, ctype = []byte(b), "text/plain; charset=utf-8"
		case nil:
		default:
			body, err = value.Marshal(b)
			ctype = "application/json"
		}
	case len(s.form) > 0:
		body, ctype = []byte(s.form.Encode()), "application/x-www-form-urlencoded"
	}
	s.paths, s.params, s.form, s.parts, s.body, s.hasBody = nil, nil, nil, nil, nil, false
	return body, ctype, err
}

// requestHeaders layers auth, content type, configured headers, and charset.
func (e *exec) requestHeaders(ctype string, hasBody bool) (map[string][]string, error) {
	headers := cloneHeader(e.sess.headers)
	if h, ok := e.sess.auth.header(); ok && headerValue(headers, "Authorization") == "" {
		headers = headerSet(headers, "Authorization", h)
	}
	if strings.HasPrefix(ctype, "multipart/") || (ctype != "" && headerValue(headers, "Content-Type") == "") {
		headers = headerSet(headers, "Content-Type", ctype)
	}
	over, err := e.vm.HeaderOverlay()
	if err != nil {
		return nil, err
	}
	for k, v := range over {
		headers = headerValues(headers, k, v)
	}
	return withCharset(headers, e.sess.charset, hasBody), nil
}

func (e *exec) observe(exch *Exchange) {
	if e.opt.OnRequest != nil {
		e.opt.OnRequest(*exch)
	}
}

// keepResponse defines the response variables and stores its cookies.
func (e *exec) keepResponse(reply Reply, full string) error {
	parsed, err := responseBody(headerValue(reply.Header, "Content-Type"), reply.Body, e.sess.strictParsing)
	if err != nil {
		return err
	}
	e.setDefined("response", parsed)
	e.setDefined("responseStatus", int64(reply.Status))
	e.setDefined("responseTime", reply.Duration.Milliseconds())
	cookieURL := reply.URL
	if cookieURL == "" {
		cookieURL = full
	}
	if u, err := url.Parse(cookieURL); err == nil {
		resp := http.Response{Header: reply.Header}
		e.sess.jar.SetCookies(u, resp.Cookies())
		e.setDefined("responseCookies", responseCookies(resp.Cookies()))
	}
	e.setDefined("responseBytes", reply.Body)
	e.setDefined("responseHeaders", headerAny(reply.Header))
	return nil
}

func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func buildURL(base string, paths []string, params url.Values) (string, error) {
	u, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("bad url: %w", err)
	}
	plain := []string{}
	raw := []string{}
	if p := strings.Trim(u.Path, "/"); p != "" {
		plain = append(plain, p)
		raw = append(raw, strings.Trim(u.EscapedPath(), "/"))
	}
	for _, p := range paths {
		p = strings.Trim(p, "/")
		if p == "" {
			continue
		}
		plain = append(plain, p)
		raw = append(raw, url.PathEscape(p))
	}
	if len(plain) > 0 {
		u.Path = "/" + strings.Join(plain, "/")
		u.RawPath = "/" + strings.Join(raw, "/")
	}
	q := u.Query()
	for k, vs := range params {
		for _, v := range vs {
			q.Add(k, v)
		}
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func responseBody(contentType string, raw []byte, strict bool) (any, error) {
	ct := strings.ToLower(contentType)
	if strict && strings.Contains(ct, "json") {
		parsed, err := value.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid JSON response: %w", err)
		}
		return parsed, nil
	}
	if strict && strings.Contains(ct, "xml") {
		parsed, err := xml.ParseDocument(string(raw))
		if err != nil {
			return nil, fmt.Errorf("invalid XML response: %w", err)
		}
		return parsed, nil
	}
	return parseBody(contentType, raw), nil
}

func parseBody(contentType string, raw []byte) any {
	trim := bytes.TrimSpace(raw)
	if len(trim) == 0 {
		return nil
	}
	ct := strings.ToLower(contentType)
	if strings.Contains(ct, "xml") || trim[0] == '<' {
		if doc, err := xml.ParseDocument(string(raw)); err == nil {
			return doc
		}
	}
	looksJSON := strings.Contains(ct, "json") || trim[0] == '{' || trim[0] == '['
	if looksJSON {
		if v, err := value.Parse(trim); err == nil {
			return v
		}
	}
	return string(raw)
}

func cloneHeader(h map[string][]string) map[string][]string {
	return http.Header(h).Clone()
}

func headerSet(h map[string][]string, key, val string) map[string][]string {
	if h == nil {
		h = map[string][]string{}
	}
	for k := range h {
		if strings.EqualFold(k, key) {
			h[k] = []string{val}
			return h
		}
	}
	h[key] = []string{val}
	return h
}

func headerValue(h map[string][]string, key string) string {
	for k, vs := range h {
		if strings.EqualFold(k, key) && len(vs) > 0 {
			return vs[0]
		}
	}
	return ""
}

// withCharset adds the configured charset to a text Content-Type that has none.
func withCharset(h map[string][]string, charset string, hasBody bool) map[string][]string {
	ct := headerValue(h, "Content-Type")
	if charset == "" || !hasBody || ct == "" || strings.Contains(strings.ToLower(ct), "charset=") ||
		strings.HasPrefix(ct, "multipart/") || ct == "application/octet-stream" {
		return h
	}
	return headerSet(h, "Content-Type", ct+"; charset="+charset)
}

func secretHeader(k string) bool {
	switch strings.ToLower(k) {
	case "authorization", "proxy-authorization", "cookie", "set-cookie":
		return true
	default:
		return false
	}
}

func headerAny(h map[string][]string) map[string]any {
	out := map[string]any{}
	for k, vs := range h {
		values := append([]string(nil), vs...)
		out[k] = values
		out[http.CanonicalHeaderKey(k)] = values
	}
	return out
}
