package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gi-runner/gi/internal/suite"
	"github.com/gi-runner/gi/internal/value"
	"github.com/gi-runner/gi/internal/xml"
)

func (e *exec) sleep(d time.Duration, wake <-chan struct{}) error {
	if d < 0 {
		return errors.New("pause expects a nonnegative number of milliseconds")
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-e.ctx.Done():
		return e.ctx.Err()
	case <-wake:
		return suite.ErrYield
	}
}

func (e *exec) logLine(line string) error {
	if e.opt.Stdout == nil || e.opt.LoadMode || e.opt.reportDisabled {
		return nil
	}
	_, err := fmt.Fprintln(e.opt.Stdout, line)
	if err != nil {
		return fmt.Errorf("log: %w", err)
	}
	return nil
}

func (e *exec) info() map[string]any {
	name := ""
	if e.scenario != nil {
		name = e.scenario.Name
	}
	return map[string]any{
		"scenarioName":        name,
		"scenarioDescription": "",
		"featureDir":          filepath.Dir(e.file.Path),
		"featureFileName":     filepath.Base(e.file.Path),
	}
}

func (e *exec) scenarioInfo() map[string]any {
	if e.scenario == nil {
		return map[string]any{}
	}
	return map[string]any{"name": e.scenario.Name, "line": e.scenario.Line}
}

func (e *exec) featureInfo() map[string]any {
	return map[string]any{
		"name":      e.file.Name,
		"fileName":  filepath.Base(e.file.Path),
		"parentDir": filepath.Dir(e.file.Path),
	}
}

func (e *exec) writeFile(val any, rel string) (string, error) {
	rel = strings.TrimSpace(rel)
	if rel == "" || strings.Contains(rel, "\x00") {
		return "", errors.New("write requires a path")
	}
	clean := filepath.Clean(rel)
	if filepath.IsAbs(clean) {
		return "", errors.New("write path must stay under target")
	}
	root := filepath.Join(e.root(), "target")
	full := filepath.Join(root, clean)
	escaped, err := filepath.Rel(root, full)
	if err != nil || escaped == ".." || strings.HasPrefix(escaped, ".."+string(filepath.Separator)) {
		return "", errors.New("write path must stay under target")
	}
	body, err := writeBytes(val)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		return "", err
	}
	if err := os.WriteFile(full, body, 0o600); err != nil {
		return "", err
	}
	return full, nil
}

func writeBytes(val any) ([]byte, error) {
	switch x := val.(type) {
	case nil:
		return []byte("null"), nil
	case string:
		return []byte(x), nil
	case []byte:
		return append([]byte(nil), x...), nil
	case *xml.Document:
		return []byte(x.String()), nil
	default:
		return json.Marshal(val)
	}
}

func (e *exec) waitHTTP(raw string, timeout, every time.Duration, wake <-chan struct{}) error {
	if every <= 0 {
		every = 250 * time.Millisecond
	}
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: every}
	var last error
	for {
		if err := e.ctx.Err(); err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(e.ctx, http.MethodGet, raw, nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 400 {
				return nil
			}
			last = fmt.Errorf("status %d", resp.StatusCode)
		} else {
			last = err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("HTTP endpoint not available: %s: %w", raw, last)
		}
		timer := time.NewTimer(every)
		select {
		case <-e.ctx.Done():
			timer.Stop()
			return e.ctx.Err()
		case <-wake:
			timer.Stop()
			return suite.ErrYield
		case <-timer.C:
		}
	}
}

func (e *exec) waitPort(host string, port int, timeout, every time.Duration, wake <-chan struct{}) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("port %d is not valid", port)
	}
	if every <= 0 {
		every = 250 * time.Millisecond
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	dialer := net.Dialer{Timeout: every}
	deadline := time.Now().Add(timeout)
	var last error
	for {
		if err := e.ctx.Err(); err != nil {
			return err
		}
		conn, err := dialer.DialContext(e.ctx, "tcp", addr)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		last = err
		if time.Now().After(deadline) {
			return fmt.Errorf("port %s not available: %w", addr, last)
		}
		timer := time.NewTimer(every)
		select {
		case <-e.ctx.Done():
			timer.Stop()
			return e.ctx.Err()
		case <-wake:
			timer.Stop()
			return suite.ErrYield
		case <-timer.C:
		}
	}
}

func (e *exec) xmlDoc(name, base string) (*xml.Document, error) {
	if raw, ok := e.vm.Get(name); ok {
		doc, ok := raw.(*xml.Document)
		if !ok {
			return nil, fmt.Errorf("%s is not XML", name)
		}
		return doc, nil
	}
	root := strings.TrimPrefix(base, "/")
	if i := strings.IndexAny(root, "/["); i >= 0 {
		root = root[:i]
	}
	doc, err := xml.ParseDocument("<" + root + "/>")
	if err != nil {
		return nil, err
	}
	if err := e.assign(name, doc); err != nil {
		return nil, err
	}
	return doc, nil
}

func (e *exec) setXMLTable(name, base string, rows [][]string) error {
	if len(rows) < 2 {
		return errors.New("set table requires a path column")
	}
	doc, err := e.xmlDoc(name, base)
	if err != nil {
		return err
	}
	headers := rows[0]
	pathCol := max(slices.Index(headers, "path"), 0)
	var indexed []int
	for i, header := range headers {
		if i == pathCol {
			continue
		}
		if _, err := strconv.Atoi(strings.TrimSpace(header)); err == nil {
			indexed = append(indexed, i)
		}
	}
	for _, row := range rows[1:] {
		if pathCol >= len(row) {
			continue
		}
		rowPath := strings.TrimSpace(row[pathCol])
		if len(indexed) > 0 {
			for _, col := range indexed {
				if col >= len(row) || strings.TrimSpace(row[col]) == "" {
					continue
				}
				full := base + "[" + strings.TrimSpace(headers[col]) + "]"
				if rowPath != "" {
					full += "/" + strings.TrimPrefix(rowPath, "/")
				}
				if err := e.setXMLValue(doc, full, row[col]); err != nil {
					return err
				}
			}
			continue
		}
		valueCol := slices.Index(headers, "value")
		if valueCol < 0 {
			valueCol = 1
		}
		if valueCol >= len(row) || strings.TrimSpace(row[valueCol]) == "" {
			continue
		}
		full := base
		if rowPath != "" {
			full += "/" + strings.TrimPrefix(rowPath, "/")
		}
		if err := e.setXMLValue(doc, full, row[valueCol]); err != nil {
			return err
		}
	}
	return nil
}

func (e *exec) setXMLValue(doc *xml.Document, path, expr string) error {
	val, err := e.eval(strings.TrimSpace(expr))
	if err != nil {
		return err
	}
	if s, ok := val.(string); ok {
		return doc.Set(path, s)
	}
	return doc.Set(path, value.Sprint(val))
}

// configuration returns a detached snapshot of the effective HTTP settings.
// Callback configuration and unimplemented Java settings are not exposed.
func (e *exec) configuration() map[string]any {
	return map[string]any{
		"timeout":               e.sess.timeout.Milliseconds(),
		"readTimeout":           e.sess.timeout.Milliseconds(),
		"connectTimeout":        e.sess.transport.ConnectTimeout.Milliseconds(),
		"followRedirects":       e.sess.follow,
		"strictResponseParsing": e.sess.strictParsing,
	}
}
