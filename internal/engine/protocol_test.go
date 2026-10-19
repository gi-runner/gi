package engine_test

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/gi-runner/gi/internal/engine"
	"github.com/gi-runner/gi/internal/feature"
	"github.com/gi-runner/gi/internal/grpcx"
	"github.com/gi-runner/gi/internal/kafkax"
	"github.com/gi-runner/gi/internal/wsx"
	"github.com/twmb/franz-go/pkg/kfake"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
)

func TestXMLAndBrowserSteps(t *testing.T) {
	page := &fakePage{}
	res, err := engine.RunFile(context.Background(), parse(t, `
Feature: page
Scenario: read
* xml user =
"""
<user id="1"><name>Ada</name></user>
"""
* match user.user.name == 'Ada'
* match user.user['@id'] == '1'
* xmlstring out = user
* match out contains '<name>Ada</name>'
* driver pageUrl
* input '#name' = 'Ada'
* click '#go'
* def got = gi.pageText('#out')
* match got == 'Ada'
`), engine.Options{
		Vars: map[string]any{"pageUrl": "http://page.test"},
		NewPage: func(context.Context) (engine.Page, error) {
			return page, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Passed() {
		t.Fatal(res.Scenarios[0].Error)
	}
	if page.url != "http://page.test" || page.filled != "Ada" || !page.clicked {
		t.Fatalf("%#v", page)
	}
}

func TestWebSocketChannel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer func() { _ = c.Close(websocket.StatusNormalClosure, "") }()
		_, b, err := c.Read(r.Context())
		if err != nil {
			return
		}
		_ = c.Write(r.Context(), websocket.MessageText, b)
	}))
	defer srv.Close()
	res, err := engine.RunFile(context.Background(), parse(t, `
Feature: socket
Scenario: echo
* def sock = gi.channel('websocket', { url: wsUrl })
* def sent = sock.send('ping')
* def got = sock.receive()
* match got == 'ping'
`), engine.Options{
		Vars: map[string]any{"wsUrl": "ws" + strings.TrimPrefix(srv.URL, "http")},
		Dial: func(ctx context.Context, kind string, spec map[string]any) (engine.Link, error) {
			return wsx.Dial(ctx, spec)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Passed() {
		t.Fatal(res.Scenarios[0].Error)
	}
}

func TestGRPCChannel(t *testing.T) {
	gs := grpc.NewServer()
	hs := health.NewServer()
	hs.SetServingStatus("gi", healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(gs, hs)
	reflection.Register(gs)
	lis := listen(t)
	defer func() { _ = lis.Close() }()
	go func() {
		if err := gs.Serve(lis); err != nil {
			return
		}
	}()
	defer gs.Stop()
	res, err := engine.RunFile(context.Background(), parse(t, `
Feature: rpc
Scenario: health
* def rpc = gi.channel('grpc', { addr: addr })
* def res = rpc.call('grpc.health.v1.Health/Check', { service: 'gi' })
* match res.status == 'SERVING'
`), engine.Options{
		Vars: map[string]any{"addr": lis.Addr().String()},
		Dial: func(ctx context.Context, kind string, spec map[string]any) (engine.Link, error) {
			return grpcx.Dial(ctx, spec)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Passed() {
		t.Fatal(res.Scenarios[0].Error)
	}
}

func TestKafkaChannel(t *testing.T) {
	cluster, err := kfake.NewCluster(kfake.NumBrokers(1), kfake.SeedTopics(1, "events"))
	if err != nil {
		t.Fatal(err)
	}
	defer cluster.Close()
	res, err := engine.RunFile(context.Background(), parse(t, `
Feature: bus
Scenario: round trip
* def bus = gi.channel('kafka', { brokers: addr, topic: 'events' })
* def sent = bus.send('one')
* def sent2 = bus.send('two')
* def a = bus.receive()
* def b = bus.receive()
* match a == 'one'
* match b == 'two'
`), engine.Options{
		Timeout: 5 * time.Second,
		Vars:    map[string]any{"addr": cluster.ListenAddrs()[0]},
		Dial: func(ctx context.Context, kind string, spec map[string]any) (engine.Link, error) {
			return kafkax.Dial(ctx, spec)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Passed() {
		t.Fatal(res.Scenarios[0].Error)
	}
}

func TestGRPCStreamAndKafkaSASL(t *testing.T) {
	gs := grpc.NewServer()
	hs := health.NewServer()
	hs.SetServingStatus("gi", healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(gs, hs)
	reflection.Register(gs)
	lis := listen(t)
	defer func() { _ = lis.Close() }()
	go func() {
		if err := gs.Serve(lis); err != nil {
			return
		}
	}()
	defer gs.Stop()
	cluster, err := kfake.NewCluster(
		kfake.NumBrokers(1),
		kfake.SeedTopics(1, "events"),
		kfake.EnableSASL(),
		kfake.Superuser("SCRAM-SHA-256", "ada", "secret"),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer cluster.Close()
	res, err := engine.RunFile(context.Background(), parse(t, `
Feature: secured protocols
Scenario: stream and sasl
* def rpc = gi.channel('grpc', { addr: addr })
* def watch = rpc.open('grpc.health.v1.Health/Watch')
* watch.send({ service: 'gi' })
* watch.closeSend()
* def first = watch.receive()
* match first.status == 'SERVING'
* watch.close()
* def bus = gi.channel('kafka', { brokers: brokers, topic: 'events', sasl: { mechanism: 'SCRAM-SHA-256', username: 'ada', password: 'secret' } })
* def sent = bus.send('secured')
* def got = bus.receive()
* match got == 'secured'
`), engine.Options{
		Timeout: 5 * time.Second,
		Vars: map[string]any{
			"addr":    lis.Addr().String(),
			"brokers": cluster.ListenAddrs()[0],
		},
		Dial: func(ctx context.Context, kind string, spec map[string]any) (engine.Link, error) {
			switch kind {
			case "grpc":
				return grpcx.Dial(ctx, spec)
			case "kafka":
				return kafkax.Dial(ctx, spec)
			default:
				t.Fatalf("kind %s", kind)
				return engine.Link{}, nil
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Passed() {
		t.Fatal(res.Scenarios[0].Error)
	}
}

type fakePage struct {
	url     string
	filled  string
	clicked bool
	text    string
}

func (p *fakePage) Open(_ context.Context, url string) error {
	p.url = url
	return nil
}

func parse(t *testing.T, src string) *feature.File {
	t.Helper()
	f, err := feature.Parse("protocol.feature", src)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func listen(t *testing.T) net.Listener {
	t.Helper()
	lis, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	return lis
}

func (p *fakePage) Click(context.Context, string) error {
	p.clicked = true
	p.text = p.filled
	return nil
}

func (p *fakePage) Fill(_ context.Context, _, text string) error {
	p.filled = text
	return nil
}

func (p *fakePage) Text(context.Context, string) (string, error) { return p.text, nil }

func (p *fakePage) Close() error { return nil }
