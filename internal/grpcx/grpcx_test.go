package grpcx

import (
	"context"
	"crypto/tls"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gi-runner/gi/internal/value"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/reflection"
	reflectionv1 "google.golang.org/grpc/reflection/grpc_reflection_v1"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

func TestTLSMetadataAndUnaryCalls(t *testing.T) {
	// Reuse the standard library's local test certificate; never change global trust.
	certServer := httptest.NewTLSServer(http.NotFoundHandler())
	pair := certServer.TLS.Certificates[0]
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certServer.Certificate().Raw})
	certServer.Close()
	certPath := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(certPath, ca, 0600); err != nil {
		t.Fatal(err)
	}
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	server := grpc.NewServer(
		grpc.Creds(credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{pair}})),
		grpc.UnaryInterceptor(func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
			md, _ := metadata.FromIncomingContext(ctx)
			if got := md.Get("x-test"); len(got) != 2 || got[0] != "one" || got[1] != "two" {
				t.Errorf("metadata: %v", md)
			}
			calls.Add(1)
			return handler(ctx, req)
		}),
	)
	healthServer := health.NewServer()
	healthServer.SetServingStatus("gi", healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(server, healthServer)
	reflection.Register(server)
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); <-done })
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	link, err := Dial(ctx, map[string]any{
		"addr":     listener.Addr().String(),
		"tls":      map[string]any{"caCert": certPath},
		"metadata": map[string]any{"x-test": []any{"one", "two"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := link.Close(); err != nil {
			t.Error(err)
		}
	}()
	for range 2 {
		got, err := link.Call(ctx, "grpc.health.v1.Health/Check", map[string]any{"service": "gi"})
		if err != nil {
			t.Fatal(err)
		}
		result, ok := value.AsMap(got)
		if !ok || result["status"] != "SERVING" {
			t.Fatalf("response: %#v", got)
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("calls: %d", calls.Load())
	}
	if _, err := link.Call(ctx, "grpc.health.v1.Health/Watch", map[string]any{"service": "gi"}); err == nil {
		t.Fatal("streaming method accepted by unary API")
	}
	if _, err := link.Open(ctx, "grpc.health.v1.Health/Check"); err == nil {
		t.Fatal("unary method accepted by streaming API")
	}
}

func TestServerClientAndBidiStreams(t *testing.T) {
	addr, stop := serveEcho(t)
	defer stop()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	link, err := Dial(ctx, map[string]any{"addr": addr})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := link.Close(); err != nil {
			t.Error(err)
		}
	}()

	watch, err := link.Open(ctx, "grpc.health.v1.Health/Watch")
	if err != nil {
		t.Fatal(err)
	}
	if err := watch.Send(ctx, map[string]any{"service": "gi"}); err != nil {
		t.Fatal(err)
	}
	got, err := watch.Receive(ctx)
	if err != nil {
		t.Fatal(err)
	}
	status, _ := value.AsMap(got)
	if status["status"] != "SERVING" {
		t.Fatalf("watch: %#v", got)
	}
	if err := watch.Close(); err != nil {
		t.Fatal(err)
	}

	upload, err := link.Open(ctx, "gi.echo.Echo/Upload")
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"aa", "bb"} {
		if err := upload.Send(ctx, map[string]any{"text": text}); err != nil {
			t.Fatal(err)
		}
	}
	if err := upload.CloseSend(); err != nil {
		t.Fatal(err)
	}
	got, err = upload.Receive(ctx)
	if err != nil {
		t.Fatal(err)
	}
	note, _ := value.AsMap(got)
	if note["text"] != "aa bb" {
		t.Fatalf("upload: %#v", got)
	}
	if _, err := upload.Receive(ctx); err == nil || !strings.Contains(err.Error(), "grpc stream closed") {
		t.Fatalf("upload end: %v", err)
	}

	chat, err := link.Open(ctx, "gi.echo.Echo/Chat")
	if err != nil {
		t.Fatal(err)
	}
	if err := chat.Send(ctx, map[string]any{"text": "ping"}); err != nil {
		t.Fatal(err)
	}
	got, err = chat.Receive(ctx)
	if err != nil {
		t.Fatal(err)
	}
	note, _ = value.AsMap(got)
	if note["text"] != "ping" {
		t.Fatalf("chat: %#v", got)
	}
	if err := chat.CloseSend(); err != nil {
		t.Fatal(err)
	}
	if _, err := chat.Receive(ctx); err == nil || !strings.Contains(err.Error(), "grpc stream closed") {
		t.Fatalf("chat end: %v", err)
	}
}

func serveEcho(t *testing.T) (string, func()) {
	t.Helper()
	file := echoFile(t)
	files := &protoregistry.Files{}
	if err := files.RegisterFile(file); err != nil {
		t.Fatal(err)
	}
	note := file.Messages().ByName("Note")
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	healthServer := health.NewServer()
	healthServer.SetServingStatus("gi", healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(server, healthServer)
	server.RegisterService(&grpc.ServiceDesc{
		ServiceName: "gi.echo.Echo",
		Streams: []grpc.StreamDesc{
			{StreamName: "Upload", Handler: uploadNotes(note), ClientStreams: true},
			{StreamName: "Chat", Handler: echoNotes(note), ClientStreams: true, ServerStreams: true},
		},
	}, nil)
	reflectionv1.RegisterServerReflectionServer(server, reflection.NewServerV1(reflection.ServerOptions{
		Services:           server,
		DescriptorResolver: joinedFiles{extra: files},
	}))
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	return listener.Addr().String(), func() { server.Stop(); <-done }
}

type joinedFiles struct{ extra *protoregistry.Files }

func (f joinedFiles) FindFileByPath(path string) (protoreflect.FileDescriptor, error) {
	if fd, err := f.extra.FindFileByPath(path); err == nil {
		return fd, nil
	}
	return protoregistry.GlobalFiles.FindFileByPath(path)
}

func (f joinedFiles) FindDescriptorByName(name protoreflect.FullName) (protoreflect.Descriptor, error) {
	if d, err := f.extra.FindDescriptorByName(name); err == nil {
		return d, nil
	}
	return protoregistry.GlobalFiles.FindDescriptorByName(name)
}

func echoFile(t *testing.T) protoreflect.FileDescriptor {
	t.Helper()
	kind := descriptorpb.FieldDescriptorProto_TYPE_STRING
	label := descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL
	fd := &descriptorpb.FileDescriptorProto{
		Name:    new("echo.proto"),
		Package: new("gi.echo"),
		Syntax:  new("proto3"),
		MessageType: []*descriptorpb.DescriptorProto{{
			Name: new("Note"),
			Field: []*descriptorpb.FieldDescriptorProto{{
				Name:     new("text"),
				JsonName: new("text"),
				Number:   new(int32(1)),
				Label:    &label,
				Type:     &kind,
			}},
		}},
		Service: []*descriptorpb.ServiceDescriptorProto{{
			Name: new("Echo"),
			Method: []*descriptorpb.MethodDescriptorProto{
				{
					Name:            new("Upload"),
					InputType:       new(".gi.echo.Note"),
					OutputType:      new(".gi.echo.Note"),
					ClientStreaming: new(true),
				},
				{
					Name:            new("Chat"),
					InputType:       new(".gi.echo.Note"),
					OutputType:      new(".gi.echo.Note"),
					ClientStreaming: new(true),
					ServerStreaming: new(true),
				},
			},
		}},
	}
	file, err := protodesc.NewFile(fd, nil)
	if err != nil {
		t.Fatal(err)
	}
	return file
}

func uploadNotes(note protoreflect.MessageDescriptor) grpc.StreamHandler {
	return func(_ any, stream grpc.ServerStream) error {
		var parts []string
		for {
			msg := dynamicpb.NewMessage(note)
			err := stream.RecvMsg(msg)
			if errors.Is(err, io.EOF) {
				out := dynamicpb.NewMessage(note)
				out.Set(note.Fields().ByName("text"), protoreflect.ValueOfString(strings.Join(parts, " ")))
				return stream.SendMsg(out)
			}
			if err != nil {
				return err
			}
			parts = append(parts, msg.Get(note.Fields().ByName("text")).String())
		}
	}
}

func echoNotes(note protoreflect.MessageDescriptor) grpc.StreamHandler {
	return func(_ any, stream grpc.ServerStream) error {
		for {
			msg := dynamicpb.NewMessage(note)
			err := stream.RecvMsg(msg)
			if errors.Is(err, io.EOF) {
				return nil
			}
			if err != nil {
				return err
			}
			if err := stream.SendMsg(msg); err != nil {
				return err
			}
		}
	}
}

func TestInvalidTLSAndMetadata(t *testing.T) {
	for _, spec := range []map[string]any{
		{"addr": "localhost:1", "tls": "yes"},
		{"addr": "localhost:1", "tls": map[string]any{"cert": "only-cert"}},
		{"addr": "localhost:1", "metadata": "token"},
	} {
		if _, err := Dial(t.Context(), spec); err == nil {
			t.Errorf("accepted invalid configuration: %#v", spec)
		}
	}
}
