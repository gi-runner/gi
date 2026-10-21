package httpx

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/gi-runner/gi/internal/engine"
	"github.com/gi-runner/gi/internal/tlsconfig"
)

func (c *client) transport(settings engine.TransportSettings) (http.RoundTripper, error) {
	if settings == (engine.TransportSettings{}) {
		return c.c.Transport, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if t := c.transports[settings]; t != nil {
		return t, nil
	}
	base := c.c.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	standard, ok := base.(*http.Transport)
	if !ok {
		return nil, errors.New("HTTP transport configuration requires a standard net/http Transport")
	}
	t := standard.Clone()
	if settings.TLSConfigured || settings.TrustAll || settings.TLSVersion != 0 || settings.CACert != "" || settings.Cert != "" || settings.Key != "" {
		conf, err := tlsconfig.Build(t.TLSClientConfig, tlsconfig.Settings{CACert: settings.CACert, Cert: settings.Cert, Key: settings.Key})
		if err != nil {
			return nil, fmt.Errorf("HTTP %w", err)
		}
		conf.InsecureSkipVerify = settings.TrustAll
		if settings.TLSVersion != 0 {
			conf.MinVersion = settings.TLSVersion
			conf.MaxVersion = settings.TLSVersion
		}
		t.TLSClientConfig = conf
	}
	if settings.ProxySet {
		if settings.Proxy == "" {
			t.Proxy = nil
		} else {
			proxy, err := url.Parse(settings.Proxy)
			if err != nil {
				return nil, err
			}
			t.Proxy = func(r *http.Request) (*url.URL, error) {
				for pattern := range strings.SplitSeq(settings.NonProxyHosts, "|") {
					if pattern != "" {
						matched, err := path.Match(pattern, r.URL.Hostname())
						if err != nil {
							return nil, err
						}
						if matched {
							return nil, nil //nolint:nilnil // A nil proxy URL means connect directly in net/http.
						}
					}
				}
				return proxy, nil
			}
		}
	}
	if settings.ConnectTimeout > 0 {
		t.DialContext = (&net.Dialer{Timeout: settings.ConnectTimeout, KeepAlive: 30 * time.Second}).DialContext
	}
	if c.transports == nil {
		c.transports = make(map[engine.TransportSettings]*http.Transport)
	}
	c.transports[settings] = t
	return t, nil
}

// CloseIdleConnections closes only transports allocated by this adapter. The
// caller still owns its supplied client and transport.
func (c *client) CloseIdleConnections() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, t := range c.transports {
		t.CloseIdleConnections()
	}
}
