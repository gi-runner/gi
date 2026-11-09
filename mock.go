package gi

import (
	"context"
	"sync"

	"github.com/gi-runner/gi/internal/engine"
	"github.com/gi-runner/gi/internal/mock"
)

// MockOptions configures a mock server. Port 0 picks a free port.
// SSL serves a self-signed certificate unless Cert and Key are set.
type MockOptions struct {
	Port       int
	SSL        bool
	Cert, Key  string
	PathPrefix string
	Arg        map[string]any
}

// Server is a running mock. It stops on Close or when its context ends.
type Server struct {
	URL  string
	Port int
	stop func() error
}

// Close stops the server and waits up to five seconds for open requests.
func (s *Server) Close() error { return s.stop() }

// Mock serves a mock feature file, matching each request to its scenarios.
func Mock(ctx context.Context, path string, opt MockOptions) (*Server, error) {
	srv, err := mock.Start(ctx, engine.MockSpec{
		Path: path, Port: opt.Port, SSL: opt.SSL, Cert: opt.Cert, Key: opt.Key, Arg: opt.Arg, PathPrefix: opt.PathPrefix,
	})
	if err != nil {
		return nil, err
	}
	stop := sync.OnceValue(srv.Stop)
	after := context.AfterFunc(ctx, func() { _ = stop() })
	return &Server{URL: srv.URL, Port: srv.Port, stop: func() error {
		after()
		return stop()
	}}, nil
}
