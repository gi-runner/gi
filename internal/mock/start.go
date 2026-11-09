package mock

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/gi-runner/gi/internal/engine"
	"github.com/gi-runner/gi/internal/feature"
	"github.com/gi-runner/gi/internal/script"
)

// Start serves a mock feature on its own port until Stop.
func Start(ctx context.Context, spec engine.MockSpec) (script.Server, error) {
	f, err := feature.ParseFile(spec.Path)
	if err != nil {
		return script.Server{}, err
	}
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", net.JoinHostPort("", strconv.Itoa(spec.Port)))
	if err != nil {
		return script.Server{}, err
	}
	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		_ = ln.Close()
		return script.Server{}, errors.New("mock listener has no TCP port")
	}
	scheme := "http"
	if spec.SSL || spec.Cert != "" {
		cfg, err := serverTLS(spec.Cert, spec.Key)
		if err != nil {
			_ = ln.Close()
			return script.Server{}, err
		}
		ln = tls.NewListener(ln, cfg)
		scheme = "https"
	}
	srv := &http.Server{
		Handler:           New(ctx, f, Options{Arg: spec.Arg, PathPrefix: spec.PathPrefix}),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() { _ = srv.Serve(ln) }()
	// The server outlives the starting step; scenario cleanup calls Stop.
	life := context.WithoutCancel(ctx)
	return script.Server{
		URL:  fmt.Sprintf("%s://localhost:%d", scheme, addr.Port),
		Port: addr.Port,
		SSL:  scheme == "https",
		Stop: func() error {
			done, cancel := context.WithTimeout(life, 5*time.Second)
			defer cancel()
			return srv.Shutdown(done)
		},
	}, nil
}

func serverTLS(cert, key string) (*tls.Config, error) {
	if cert != "" || key != "" {
		if cert == "" || key == "" {
			return nil, errors.New("mock TLS needs both cert and key")
		}
		pair, err := tls.LoadX509KeyPair(cert, key)
		if err != nil {
			return nil, err
		}
		return &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS12}, nil
	}
	pair, err := selfSigned()
	if err != nil {
		return nil, err
	}
	return &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS12}, nil
}

func selfSigned() (tls.Certificate, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "localhost"},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: priv}, nil
}
