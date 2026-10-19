package engine

import (
	"crypto/tls"
	"errors"
	"fmt"
	"math"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/gi-runner/gi/internal/suite"
	"github.com/gi-runner/gi/internal/value"
)

// TransportSettings is immutable data, comparable for connection-pool reuse.
// Default settings preserve the caller's HTTP client's transport.
type TransportSettings struct {
	TLSConfigured        bool
	TrustAll             bool
	TLSVersion           uint16
	CACert, Cert, Key    string
	ProxySet             bool
	Proxy, NonProxyHosts string
	ConnectTimeout       time.Duration
}

func (e *exec) configureSSL(val any) error {
	next := e.sess.transport
	next.TLSConfigured = val != nil
	next.TrustAll = false
	next.TLSVersion = 0
	next.CACert = ""
	next.Cert = ""
	next.Key = ""
	settings := map[string]any{}
	switch x := val.(type) {
	case nil:
	case bool:
		next.TrustAll = x
	case string:
		next.TrustAll = true
		settings["algorithm"] = x
	case map[string]any:
		settings = x
		next.TrustAll = true
	default:
		return errors.New("ssl expects a boolean, protocol name, or object")
	}
	resolve := func(v any) string {
		if v == nil {
			return ""
		}
		return suite.Resolve(e.root(), filepath.Dir(e.file.Path), value.Sprint(v))
	}
	for key, v := range settings {
		switch key {
		case "trustAll":
			b, ok := v.(bool)
			if !ok {
				return errors.New("ssl trustAll expects a boolean")
			}
			next.TrustAll = b
		case "caCert":
			next.CACert = resolve(v)
		case "cert":
			next.Cert = resolve(v)
		case "key":
			next.Key = resolve(v)
		case "algorithm":
			switch value.Sprint(v) {
			case "TLS":
			case "TLSv1.2":
				next.TLSVersion = tls.VersionTLS12
			case "TLSv1.3":
				next.TLSVersion = tls.VersionTLS13
			default:
				return fmt.Errorf("unsupported TLS algorithm %q", value.Sprint(v))
			}
		default:
			return fmt.Errorf("unsupported ssl option %q; native certificate options use PEM caCert, cert, and key", key)
		}
	}
	if (next.Cert == "") != (next.Key == "") {
		return errors.New("ssl requires both cert and key")
	}
	e.sess.transport = next
	return nil
}
func (e *exec) configureProxy(val any) error {
	next := e.sess.transport
	next.ProxySet = true
	next.Proxy = ""
	next.NonProxyHosts = ""
	if val == nil {
		e.sess.transport = next
		return nil
	}
	raw, ok := val.(string)
	if !ok {
		m, ok := value.AsMap(val)
		if !ok {
			return errors.New("proxy expects a URI or object")
		}
		raw = value.Sprint(m["uri"])
		if hosts, ok := value.AsSlice(m["nonProxyHosts"]); ok {
			out := make([]string, len(hosts))
			for i, h := range hosts {
				out[i] = value.Sprint(h)
			}
			next.NonProxyHosts = strings.Join(out, "|")
		}
		if raw != "" && m["username"] != nil {
			u, err := url.Parse(raw)
			if err != nil {
				return err
			}
			u.User = url.UserPassword(value.Sprint(m["username"]), value.Sprint(m["password"]))
			raw = u.String()
		}
	}
	if raw != "" {
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "socks5" && u.Scheme != "socks5h") {
			return errors.New("proxy requires an absolute HTTP, HTTPS, or SOCKS5 URI")
		}
	}
	next.Proxy = raw
	e.sess.transport = next
	return nil
}
func (e *exec) configureConnectTimeout(val any) error {
	n, ok := value.AsInt(val)
	if !ok || n <= 0 || int64(n) > math.MaxInt64/int64(time.Millisecond) {
		return errors.New("connectTimeout expects positive milliseconds within the duration range")
	}
	e.sess.transport.ConnectTimeout = time.Duration(n) * time.Millisecond
	return nil
}
