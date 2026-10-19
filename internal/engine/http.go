package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/gi-runner/gi/internal/value"
	"github.com/gi-runner/gi/internal/xml"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
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

func (e *exec) method(ctx context.Context, name string, sr *StepResult) error {
	method := strings.ToUpper(strings.TrimSpace(name))
	switch method {
	case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS":
	default:
		v, err := e.eval(name)
		if err != nil {
			return fmt.Errorf("unknown method %q: %w", name, err)
		}
		method = strings.ToUpper(value.Sprint(v))
		if method == "" || strings.ContainsAny(method, " \t\r\n") {
			return fmt.Errorf("invalid method %q", method)
		}
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
	form := e.sess.form
	if (method == "GET" || method == "HEAD") && len(form) > 0 {
		full, err = addQuery(full, form)
		if err != nil {
			return err
		}
	}
	parts := e.sess.parts
	var body []byte
	ctype := ""
	switch {
	case len(parts) > 0 && method != "GET" && method != "HEAD":
		raw, ct, err := encodeParts(parts)
		if err != nil {
			return err
		}
		body = raw
		ctype = ct
	case e.sess.hasBody && method != "GET" && method != "HEAD":
		switch b := e.sess.body.(type) {
		case *xml.Document:
			body = []byte(b.String())
			ctype = "application/xml"
		case []byte:
			body = b
			ctype = "application/octet-stream"
		case string:
			body = []byte(b)
			ctype = "text/plain; charset=utf-8"
		case nil:
		default:
			raw, err := value.Marshal(b)
			if err != nil {
				return err
			}
			body = raw
			ctype = "application/json"
		}
	case len(form) > 0 && method != "GET" && method != "HEAD":
		body = []byte(form.Encode())
		ctype = "application/x-www-form-urlencoded"
	}
	e.sess.paths = nil
	e.sess.params = nil
	e.sess.form = nil
	e.sess.parts = nil
	e.sess.body = nil
	e.sess.hasBody = false
	if err := e.ensureToken(ctx, full); err != nil {
		return err
	}
	until, attempts, every := e.vm.TakeRetryUntil()
	var last error
	for i := range attempts {
		if i > 0 && every > 0 {
			timer := time.NewTimer(every)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
		if e.opt.BeforeRequest != nil {
			if err := e.opt.BeforeRequest(ctx); err != nil {
				return err
			}
		}
		headers := cloneHeader(e.sess.headers)
		if h, ok := e.sess.auth.header(); ok && headerValue(headers, "Authorization") == "" {
			headers = headerSet(headers, "Authorization", h)
		}
		if strings.HasPrefix(ctype, "multipart/") || (ctype != "" && headerValue(headers, "Content-Type") == "") {
			headers = headerSet(headers, "Content-Type", ctype)
		}
		over, err := e.vm.HeaderOverlay()
		if err != nil {
			return err
		}
		for k, v := range over {
			headers = headerValues(headers, k, v)
		}
		reqCtx, cancel := context.WithTimeout(ctx, e.sess.timeout)
		requestStart := time.Now()
		reply, err := e.opt.HTTP.Send(reqCtx, Call{
			Method:    method,
			URL:       full,
			Header:    headers,
			Cookies:   e.sess.cookies,
			Jar:       e.sess.jar,
			Body:      body,
			Follow:    e.sess.follow,
			Transport: e.sess.transport,
		})
		cancel()
		exch := &Exchange{
			Started:        requestStart,
			Method:         method,
			URL:            full,
			Status:         reply.Status,
			Duration:       reply.Duration,
			RequestHeaders: flattenHeader(headers),
			RequestBody:    value.Truncate(string(body), 32<<10),
			Endpoint:       endpointName(method, full),
		}
		sr.HTTP = exch
		if err != nil {
			exch.Error = err.Error()
			if e.opt.OnRequest != nil {
				e.opt.OnRequest(*exch)
			}
			last = fmt.Errorf("request failed: %w", err)
			continue
		}
		exch.ResponseHeaders = flattenHeader(reply.Header)
		exch.ResponseBody = value.Truncate(string(reply.Body), 32<<10)
		if e.opt.OnRequest != nil {
			e.opt.OnRequest(*exch)
		}
		parsed := parseBody(headerValue(reply.Header, "Content-Type"), reply.Body)
		if e.sess.strictParsing && strings.Contains(strings.ToLower(headerValue(reply.Header, "Content-Type")), "json") {
			var err error
			parsed, err = value.Parse(reply.Body)
			if err != nil {
				return fmt.Errorf("invalid JSON response: %w", err)
			}
		}
		if e.sess.strictParsing && strings.Contains(strings.ToLower(headerValue(reply.Header, "Content-Type")), "xml") {
			var err error
			parsed, err = xml.ParseDocument(string(reply.Body))
			if err != nil {
				return fmt.Errorf("invalid XML response: %w", err)
			}
		}
		e.vm.Set("response", parsed)
		e.vm.Set("responseStatus", int64(reply.Status))
		e.vm.Set("responseTime", reply.Duration.Milliseconds())
		cookieURL := reply.URL
		if cookieURL == "" {
			cookieURL = full
		}
		if u, err := url.Parse(cookieURL); err == nil {
			resp := http.Response{Header: reply.Header}
			e.sess.jar.SetCookies(u, resp.Cookies())
			e.vm.Set("responseCookies", responseCookies(resp.Cookies()))
		}
		e.vm.Set("responseBytes", append([]byte(nil), reply.Body...))
		e.vm.Set("responseHeaders", headerAny(reply.Header))
		if until == "" {
			return nil
		}
		ok, err := e.vm.Truthy(until, nil, nil)
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		last = fmt.Errorf("retry until not satisfied: %s", until)
	}
	if last == nil {
		return fmt.Errorf("retry until not satisfied: %s", until)
	}
	return last
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
	out := make(map[string][]string, len(h))
	for k, vs := range h {
		out[k] = append([]string(nil), vs...)
	}
	return out
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

func flattenHeader(h map[string][]string) map[string]string {
	out := map[string]string{}
	for k, vs := range h {
		joined := strings.Join(vs, ", ")
		if secretHeader(k) {
			joined = "***"
		}
		out[k] = joined
	}
	return out
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
