package grpcx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/gi-runner/gi/internal/engine"
	"github.com/gi-runner/gi/internal/value"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	reflectionv1 "google.golang.org/grpc/reflection/grpc_reflection_v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

var errLinkClosed = errors.New("grpc link closed")
var errStreamClosed = errors.New("grpc stream closed")

type link struct {
	conn       *grpc.ClientConn
	metadata   metadata.MD
	mu         sync.Mutex
	services   map[string]protoreflect.ServiceDescriptor
	descriptor bool
	streams    map[*rpcStream]struct{}
	closed     bool
}

func Dial(ctx context.Context, spec map[string]any) (engine.Link, error) {
	if err := ctx.Err(); err != nil {
		return engine.Link{}, err
	}
	addr := value.Sprint(spec["addr"])
	if addr == "" {
		return engine.Link{}, errors.New("grpc requires addr")
	}
	creds, err := transportCredentials(spec["tls"])
	if err != nil {
		return engine.Link{}, err
	}
	headers, err := requestMetadata(spec["metadata"])
	if err != nil {
		return engine.Link{}, err
	}
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(creds))
	if err != nil {
		return engine.Link{}, err
	}
	services := map[string]protoreflect.ServiceDescriptor{}
	described := false
	if raw, ok := spec["descriptor"]; ok {
		path, ok := raw.(string)
		if !ok || path == "" {
			_ = conn.Close()
			return engine.Link{}, errors.New("grpc descriptor must be a file path")
		}
		services, err = servicesFromDescriptor(path)
		if err != nil {
			_ = conn.Close()
			return engine.Link{}, err
		}
		described = true
	}
	l := &link{conn: conn, metadata: headers, services: services, descriptor: described}
	return engine.Link{Call: l.Call, Open: l.Open, Close: l.Close}, nil
}

func (l *link) Close() error {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil
	}
	l.closed = true
	streams := l.streams
	l.streams = nil
	l.mu.Unlock()
	for stream := range streams {
		_ = stream.close()
	}
	return l.conn.Close()
}

func (l *link) Call(ctx context.Context, method string, v any) (any, error) {
	ctx = l.outgoing(ctx)
	method, md, err := l.lookup(ctx, method)
	if err != nil {
		return nil, err
	}
	if md.IsStreamingClient() || md.IsStreamingServer() {
		return nil, errors.New("grpc streaming method; use open")
	}
	req, err := messageIn(md.Input(), v)
	if err != nil {
		return nil, err
	}
	resp := dynamicpb.NewMessage(md.Output())
	if err := l.conn.Invoke(ctx, "/"+method, req, resp); err != nil {
		return nil, err
	}
	return messageOut(resp)
}

func (l *link) Open(ctx context.Context, method string) (engine.Stream, error) {
	ctx = l.outgoing(ctx)
	method, md, err := l.lookup(ctx, method)
	if err != nil {
		return engine.Stream{}, err
	}
	if !md.IsStreamingClient() && !md.IsStreamingServer() {
		return engine.Stream{}, errors.New("grpc unary method; use call")
	}
	ctx, cancel := context.WithCancel(ctx)
	cs, err := l.conn.NewStream(ctx, &grpc.StreamDesc{
		StreamName:    string(md.Name()),
		ServerStreams: md.IsStreamingServer(),
		ClientStreams: md.IsStreamingClient(),
	}, "/"+method)
	if err != nil {
		cancel()
		return engine.Stream{}, err
	}
	s := &rpcStream{cs: cs, in: md.Input(), out: md.Output()}
	s.closeOnce = sync.OnceFunc(func() {
		cancel()
		l.mu.Lock()
		delete(l.streams, s)
		l.mu.Unlock()
	})
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		_ = s.close()
		return engine.Stream{}, errLinkClosed
	}
	if l.streams == nil {
		l.streams = make(map[*rpcStream]struct{})
	}
	l.streams[s] = struct{}{}
	l.mu.Unlock()
	return engine.Stream{Send: s.send, Receive: s.receive, CloseSend: s.closeSend, Close: s.close}, nil
}

func (l *link) outgoing(ctx context.Context) context.Context {
	return metadata.NewOutgoingContext(ctx, metadata.Join(metadataFromContext(ctx), l.metadata))
}

func (l *link) lookup(ctx context.Context, method string) (string, protoreflect.MethodDescriptor, error) {
	method = strings.TrimPrefix(method, "/")
	svc, name, ok := strings.Cut(method, "/")
	if !ok || svc == "" || name == "" {
		return "", nil, errors.New("grpc method must be service/method")
	}
	service, err := l.service(ctx, svc)
	if err != nil {
		return "", nil, err
	}
	md := service.Methods().ByName(protoreflect.Name(name))
	if md == nil {
		return "", nil, fmt.Errorf("grpc method %s not found", name)
	}
	return method, md, nil
}

type rpcStream struct {
	closeOnce func()
	cs        grpc.ClientStream
	in        protoreflect.MessageDescriptor
	out       protoreflect.MessageDescriptor
	sendMu    sync.Mutex
	recvMu    sync.Mutex
}

func (s *rpcStream) send(ctx context.Context, v any) error {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	msg, err := messageIn(s.in, v)
	if err != nil {
		return err
	}
	return s.op(ctx, func() error { return s.cs.SendMsg(msg) })
}

func (s *rpcStream) receive(ctx context.Context) (any, error) {
	s.recvMu.Lock()
	defer s.recvMu.Unlock()
	msg := dynamicpb.NewMessage(s.out)
	if err := s.op(ctx, func() error { return s.cs.RecvMsg(msg) }); err != nil {
		_ = s.close()
		return nil, err
	}
	return messageOut(msg)
}

func (s *rpcStream) closeSend() error {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	return s.cs.CloseSend()
}

func (s *rpcStream) close() error {
	s.closeOnce()
	return nil
}

// op waits for one send or receive. The stream stays open after a
// successful call. A step timeout cancels it.
func (s *rpcStream) op(ctx context.Context, fn func() error) error {
	if err := ctx.Err(); err != nil {
		_ = s.close()
		return err
	}
	done := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { _ = s.close(); close(done) })
	err := fn()
	if !stop() {
		<-done
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, io.EOF) {
		return errStreamClosed
	}
	return err
}

func messageIn(desc protoreflect.MessageDescriptor, v any) (*dynamicpb.Message, error) {
	if v == nil {
		v = map[string]any{}
	}
	raw, err := value.Marshal(v)
	if err != nil {
		return nil, err
	}
	msg := dynamicpb.NewMessage(desc)
	if err := protojson.Unmarshal(raw, msg); err != nil {
		return nil, err
	}
	return msg, nil
}

func messageOut(msg proto.Message) (any, error) {
	raw, err := protojson.Marshal(msg)
	if err != nil {
		return nil, err
	}
	return value.Parse(raw)
}

func metadataFromContext(ctx context.Context) metadata.MD {
	md, _ := metadata.FromOutgoingContext(ctx)
	return md
}

func (l *link) service(ctx context.Context, name string) (protoreflect.ServiceDescriptor, error) {
	l.mu.Lock()
	cached := l.services[name]
	described, closed := l.descriptor, l.closed
	l.mu.Unlock()
	if closed {
		return nil, errLinkClosed
	}
	if cached != nil {
		return cached, nil
	}
	if described {
		return nil, fmt.Errorf("grpc service %s is not in the descriptor", name)
	}
	files, err := descriptors(ctx, l.conn, name)
	if err != nil {
		return nil, err
	}
	desc, err := files.FindDescriptorByName(protoreflect.FullName(name))
	if err != nil {
		return nil, err
	}
	service, ok := desc.(protoreflect.ServiceDescriptor)
	if !ok {
		return nil, fmt.Errorf("%s is not a service", name)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil, errLinkClosed
	}
	l.services[name] = service
	return service, nil
}

func servicesFromDescriptor(path string) (map[string]protoreflect.ServiceDescriptor, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	set := &descriptorpb.FileDescriptorSet{}
	if err := proto.Unmarshal(raw, set); err != nil {
		return nil, fmt.Errorf("grpc descriptor: %w", err)
	}
	files, err := protodesc.NewFiles(set)
	if err != nil {
		return nil, err
	}
	out := map[string]protoreflect.ServiceDescriptor{}
	files.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		svcs := fd.Services()
		for i := range svcs.Len() {
			svc := svcs.Get(i)
			out[string(svc.FullName())] = svc
		}
		return true
	})
	if len(out) == 0 {
		return nil, errors.New("grpc descriptor has no services")
	}
	return out, nil
}

func descriptors(ctx context.Context, conn *grpc.ClientConn, symbol string) (*protoregistry.Files, error) {
	stream, err := reflectionv1.NewServerReflectionClient(conn).ServerReflectionInfo(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = stream.CloseSend() }()
	seen := map[string]*descriptorpb.FileDescriptorProto{}
	var order []string
	pending := []string{symbol}
	byName := true
	for len(pending) > 0 {
		item := pending[0]
		pending = pending[1:]
		var msg *reflectionv1.ServerReflectionRequest
		if byName {
			msg = &reflectionv1.ServerReflectionRequest{MessageRequest: &reflectionv1.ServerReflectionRequest_FileContainingSymbol{FileContainingSymbol: item}}
			byName = false
		} else {
			if _, ok := seen[item]; ok {
				continue
			}
			msg = &reflectionv1.ServerReflectionRequest{MessageRequest: &reflectionv1.ServerReflectionRequest_FileByFilename{FileByFilename: item}}
		}
		if err := stream.Send(msg); err != nil {
			return nil, err
		}
		resp, err := stream.Recv()
		if err != nil {
			return nil, err
		}
		if errResp := resp.GetErrorResponse(); errResp != nil {
			return nil, fmt.Errorf("grpc reflection: %s", errResp.GetErrorMessage())
		}
		for _, raw := range resp.GetFileDescriptorResponse().GetFileDescriptorProto() {
			fd := &descriptorpb.FileDescriptorProto{}
			if err := proto.Unmarshal(raw, fd); err != nil {
				return nil, err
			}
			if _, ok := seen[fd.GetName()]; ok {
				continue
			}
			seen[fd.GetName()] = fd
			order = append(order, fd.GetName())
			pending = append(pending, fd.GetDependency()...)
		}
	}
	set := &descriptorpb.FileDescriptorSet{}
	for _, name := range order {
		set.File = append(set.File, seen[name])
	}
	return protodesc.NewFiles(set)
}
