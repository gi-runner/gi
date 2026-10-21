package grpcx

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func nativeLink(t *testing.T, addr string) *link {
	t.Helper()
	connection, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	result := &link{conn: connection, services: map[string]protoreflect.ServiceDescriptor{}}
	t.Cleanup(func() {
		if err := result.Close(); err != nil {
			t.Error(err)
		}
	})
	return result
}

func activeStreams(l *link) int { l.mu.Lock(); defer l.mu.Unlock(); return len(l.streams) }

func TestClosedAndCancelledStreamsReleaseOwnership(t *testing.T) {
	addr, stop := serveEcho(t)
	defer stop()
	l := nativeLink(t, addr)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	for range 32 {
		stream, err := l.Open(ctx, "gi.echo.Echo/Chat")
		if err != nil {
			t.Fatal(err)
		}
		if err := stream.Close(); err != nil {
			t.Fatal(err)
		}
		if err := stream.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if n := activeStreams(l); n != 0 {
		t.Fatalf("retained %d closed streams", n)
	}
	stream, err := l.Open(ctx, "gi.echo.Echo/Chat")
	if err != nil {
		t.Fatal(err)
	}
	receiveCtx, stopReceive := context.WithTimeout(ctx, 20*time.Millisecond)
	defer stopReceive()
	if _, err := stream.Receive(receiveCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("receive timeout: %v", err)
	}
	if n := activeStreams(l); n != 0 {
		t.Fatalf("retained %d cancelled streams", n)
	}
	upload, err := l.Open(ctx, "gi.echo.Echo/Upload")
	if err != nil {
		t.Fatal(err)
	}
	if err := upload.CloseSend(); err != nil {
		t.Fatal(err)
	}
	if _, err := upload.Receive(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := upload.Receive(ctx); err == nil {
		t.Fatal("stream did not end")
	}
	if n := activeStreams(l); n != 0 {
		t.Fatalf("retained %d completed streams", n)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Open(ctx, "gi.echo.Echo/Chat"); err == nil {
		t.Fatal("closed link opened a new stream")
	}
}

func TestCloseInterruptsReflection(t *testing.T) {
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	server := grpc.NewServer(grpc.StreamInterceptor(func(_ any, stream grpc.ServerStream, _ *grpc.StreamServerInfo, _ grpc.StreamHandler) error {
		close(entered)
		<-stream.Context().Done()
		return stream.Context().Err()
	}))
	reflection.Register(server)
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	defer func() { server.Stop(); <-served }()
	l := nativeLink(t, listener.Addr().String())
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	called := make(chan error, 1)
	go func() { _, err := l.Call(ctx, "unknown.Service/Call", nil); called <- err }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("reflection did not start")
	}
	closed := make(chan error, 1)
	go func() { closed <- l.Close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close waited behind reflection I/O")
	}
	select {
	case err := <-called:
		if err == nil {
			t.Fatal("reflection succeeded after close")
		}
	case <-time.After(time.Second):
		t.Fatal("reflection ignored connection close")
	}
}

type duplexStream struct {
	grpc.ClientStream
	ctx       context.Context //nolint:containedctx // models the lifetime of a gRPC client stream.
	receiving chan struct{}
	sent      chan struct{}
}

func (s *duplexStream) SendMsg(any) error { close(s.sent); return nil }
func (s *duplexStream) RecvMsg(any) error {
	close(s.receiving)
	select {
	case <-s.sent:
		return nil
	case <-s.ctx.Done():
		return s.ctx.Err()
	}
}

func TestBidiSendWhileReceiveIsWaiting(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		transport := &duplexStream{ctx: ctx, receiving: make(chan struct{}), sent: make(chan struct{})}
		note := echoFile(t).Messages().ByName("Note")
		stream := &rpcStream{cs: transport, in: note, out: note, closeOnce: sync.OnceFunc(cancel)}
		received := make(chan error, 1)
		go func() { _, err := stream.receive(ctx); received <- err }()
		<-transport.receiving
		if err := stream.send(ctx, map[string]any{"text": "hello"}); err != nil {
			t.Fatal(err)
		}
		if err := <-received; err != nil {
			t.Fatal(err)
		}
	})
}
