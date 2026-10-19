package engine

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestStreamOpeningTimeoutAndLifetime(t *testing.T) {
	t.Run("opening is bounded", func(t *testing.T) {
		e := &exec{ctx: t.Context(), sess: session{timeout: 20 * time.Millisecond}}
		_, err := e.openStream(Link{Open: func(ctx context.Context, _ string) (Stream, error) {
			<-ctx.Done()
			return Stream{}, ctx.Err()
		}}, "service/Watch")
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("opening: %v", err)
		}
	})
	t.Run("stream survives opening timeout", func(t *testing.T) {
		e := &exec{ctx: t.Context(), sess: session{timeout: 20 * time.Millisecond}}
		var streamDone <-chan struct{}
		closed := false
		st, err := e.openStream(Link{Open: func(ctx context.Context, _ string) (Stream, error) {
			streamDone = ctx.Done()
			return Stream{Close: func() error { closed = true; return nil }}, nil
		}}, "service/Watch")
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = st.Close() }()
		select {
		case <-streamDone:
			t.Fatal("successful opening cancelled the stream")
		case <-time.After(60 * time.Millisecond):
		}
		if err := st.Close(); err != nil {
			t.Fatal(err)
		}
		select {
		case <-streamDone:
		default:
			t.Fatal("close did not cancel the stream")
		}
		if !closed {
			t.Fatal("close did not release the stream")
		}
	})
}
