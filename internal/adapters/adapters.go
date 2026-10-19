// Package adapters wires the native protocol and browser implementations.
package adapters

import (
	"context"
	"fmt"

	"github.com/gi-runner/gi/internal/browser"
	"github.com/gi-runner/gi/internal/engine"
	"github.com/gi-runner/gi/internal/grpcx"
	"github.com/gi-runner/gi/internal/kafkax"
	"github.com/gi-runner/gi/internal/wsx"
)

func Dial(ctx context.Context, kind string, spec map[string]any) (engine.Link, error) {
	switch kind {
	case "websocket", "ws":
		return wsx.Dial(ctx, spec)
	case "grpc":
		return grpcx.Dial(ctx, spec)
	case "kafka":
		return kafkax.Dial(ctx, spec)
	default:
		return engine.Link{}, fmt.Errorf("unknown channel %q", kind)
	}
}

func NewPage(ctx context.Context) (engine.Page, error) {
	return browser.Open(ctx)
}
