package kafkax

import (
	"crypto/tls"
	"errors"
	"fmt"
	"strings"

	"github.com/gi-runner/gi/internal/tlsconfig"
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
	config, err := tlsconfig.Parse(spec)
	if err != nil {
		return nil, fmt.Errorf("kafka %w", err)
	}
	return config, nil
}
