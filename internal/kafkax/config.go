package kafkax

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/gi-runner/gi/internal/value"
	"github.com/twmb/franz-go/pkg/sasl"
	"github.com/twmb/franz-go/pkg/sasl/plain"
	"github.com/twmb/franz-go/pkg/sasl/scram"
)

func saslMechanism(spec any) (sasl.Mechanism, error) {
	if spec == nil {
		return nil, nil //nolint:nilnil // SASL is optional
	}
	settings, ok := value.AsMap(spec)
	if !ok {
		return nil, errors.New("kafka sasl expects an object")
	}
	user := value.Sprint(settings["username"])
	pass := value.Sprint(settings["password"])
	if user == "" || pass == "" {
		return nil, errors.New("kafka sasl requires username and password")
	}
	switch strings.ToUpper(value.Sprint(settings["mechanism"])) {
	case "", "PLAIN":
		return plain.Auth{User: user, Pass: pass}.AsMechanism(), nil
	case "SCRAM-SHA-256":
		return scram.Auth{User: user, Pass: pass}.AsSha256Mechanism(), nil
	case "SCRAM-SHA-512":
		return scram.Auth{User: user, Pass: pass}.AsSha512Mechanism(), nil
	default:
		return nil, fmt.Errorf("kafka sasl mechanism %s is not supported", value.Sprint(settings["mechanism"]))
	}
}

func kafkaTLS(spec any) (*tls.Config, error) {
	if spec == nil || spec == false {
		return nil, nil //nolint:nilnil // TLS is optional
	}
	conf := &tls.Config{MinVersion: tls.VersionTLS12}
	if spec == true {
		return conf, nil
	}
	settings, ok := value.AsMap(spec)
	if !ok {
		return nil, errors.New("kafka tls expects a boolean or an object")
	}
	conf.ServerName = value.Sprint(settings["serverName"])
	if path := value.Sprint(settings["caCert"]); path != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("kafka CA certificate: %w", err)
		}
		roots, err := x509.SystemCertPool()
		if err != nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM(raw) {
			return nil, errors.New("kafka CA certificate contains no certificates")
		}
		conf.RootCAs = roots
	}
	cert, key := value.Sprint(settings["cert"]), value.Sprint(settings["key"])
	if cert != "" || key != "" {
		if cert == "" || key == "" {
			return nil, errors.New("kafka client TLS requires both cert and key")
		}
		pair, err := tls.LoadX509KeyPair(cert, key)
		if err != nil {
			return nil, fmt.Errorf("kafka client certificate: %w", err)
		}
		conf.Certificates = []tls.Certificate{pair}
	}
	return conf, nil
}
