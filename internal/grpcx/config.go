package grpcx

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"

	"github.com/gi-runner/gi/internal/value"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

func transportCredentials(spec any) (credentials.TransportCredentials, error) {
	if spec == nil || spec == false {
		return insecure.NewCredentials(), nil
	}
	conf := &tls.Config{MinVersion: tls.VersionTLS12}
	if spec == true {
		return credentials.NewTLS(conf), nil
	}
	settings, ok := value.AsMap(spec)
	if !ok {
		return nil, errors.New("grpc tls expects a boolean or an object")
	}
	conf.ServerName = value.Sprint(settings["serverName"])
	if path := value.Sprint(settings["caCert"]); path != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("grpc CA certificate: %w", err)
		}
		roots, err := x509.SystemCertPool()
		if err != nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM(raw) {
			return nil, errors.New("grpc CA certificate contains no certificates")
		}
		conf.RootCAs = roots
	}
	cert, key := value.Sprint(settings["cert"]), value.Sprint(settings["key"])
	if cert != "" || key != "" {
		if cert == "" || key == "" {
			return nil, errors.New("grpc client TLS requires both cert and key")
		}
		pair, err := tls.LoadX509KeyPair(cert, key)
		if err != nil {
			return nil, fmt.Errorf("grpc client certificate: %w", err)
		}
		conf.Certificates = []tls.Certificate{pair}
	}
	return credentials.NewTLS(conf), nil
}

func requestMetadata(spec any) (metadata.MD, error) {
	if spec == nil {
		return metadata.MD{}, nil
	}
	fields, ok := value.AsMap(spec)
	if !ok {
		return nil, errors.New("grpc metadata expects an object")
	}
	md := metadata.MD{}
	for name, v := range fields {
		if v == nil {
			continue
		}
		if values, ok := value.AsSlice(v); ok {
			for _, item := range values {
				if item != nil {
					md.Append(name, value.Sprint(item))
				}
			}
		} else {
			md.Append(name, value.Sprint(v))
		}
	}
	return md, nil
}
