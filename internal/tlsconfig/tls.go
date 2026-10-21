// Package tlsconfig loads the PEM certificate settings shared by native transports.
package tlsconfig

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"

	"github.com/gi-runner/gi/internal/value"
)

type Settings struct {
	ServerName string
	CACert     string
	Cert       string
	Key        string
}

// Parse accepts the optional boolean/object TLS setting used by protocol links.
func Parse(spec any) (*tls.Config, error) {
	switch spec {
	case nil, false:
		return nil, nil //nolint:nilnil // nil means the transport does not use TLS.
	case true:
		return Build(nil, Settings{})
	}
	settings, ok := value.AsMap(spec)
	if !ok {
		return nil, errors.New("tls expects a boolean or an object")
	}
	return Build(nil, Settings{ServerName: value.Sprint(settings["serverName"]), CACert: value.Sprint(settings["caCert"]), Cert: value.Sprint(settings["cert"]), Key: value.Sprint(settings["key"])})
}

// Build preserves the caller's TLS policy without modifying its config or pools.
func Build(base *tls.Config, settings Settings) (*tls.Config, error) {
	if (settings.Cert == "") != (settings.Key == "") {
		return nil, errors.New("client TLS requires both cert and key")
	}
	config := &tls.Config{MinVersion: tls.VersionTLS12}
	if base != nil {
		config = base.Clone()
	}
	if settings.ServerName != "" {
		config.ServerName = settings.ServerName
	}
	if settings.CACert != "" {
		raw, err := os.ReadFile(settings.CACert)
		if err != nil {
			return nil, fmt.Errorf("CA certificate: %w", err)
		}
		var roots *x509.CertPool
		if config.RootCAs != nil {
			roots = config.RootCAs.Clone()
		} else {
			roots, err = x509.SystemCertPool()
			if err != nil {
				roots = x509.NewCertPool()
			}
		}
		if !roots.AppendCertsFromPEM(raw) {
			return nil, errors.New("CA certificate contains no certificates")
		}
		config.RootCAs = roots
	}
	if settings.Cert != "" {
		pair, err := tls.LoadX509KeyPair(settings.Cert, settings.Key)
		if err != nil {
			return nil, fmt.Errorf("client certificate: %w", err)
		}
		config.Certificates = []tls.Certificate{pair}
	}
	return config, nil
}
