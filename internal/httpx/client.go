package httpx

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/gi-runner/gi/internal/engine"
)

var responseBufPool = sync.Pool{
	New: func() any {
		return new([32768]byte)
	},
}

type client struct {
	c          *http.Client
	mu         sync.Mutex
	transports map[engine.TransportSettings]*http.Transport
}

func New(c *http.Client) *client {
	return &client{c: cmp.Or(c, http.DefaultClient)}
}

func Pool(vus int) *http.Client {
	vus = max(vus, 2)
	var base *http.Transport
	if t, ok := http.DefaultTransport.(*http.Transport); ok {
		base = t.Clone()
	} else {
		base = &http.Transport{}
	}
	base.MaxIdleConns = vus * 2
	base.MaxIdleConnsPerHost = vus * 2
	base.IdleConnTimeout = 90 * time.Second
	return &http.Client{Transport: base}
}

func (c *client) Send(ctx context.Context, call engine.Call) (engine.Reply, error) {
	var body io.Reader
	if len(call.Body) > 0 {
		body = bytes.NewReader(call.Body)
	}
	req, err := http.NewRequestWithContext(ctx, call.Method, call.URL, body)
	if err != nil {
		return engine.Reply{}, err
	}
	for k, vs := range call.Header {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	for _, ck := range call.Cookies {
		req.AddCookie(&http.Cookie{ //nolint:gosec // Request Cookie headers do not carry Secure/HttpOnly/SameSite attributes.
			Name: ck[0], Value: ck[1],
		})
	}
	transport, err := c.transport(call.Transport)
	if err != nil {
		return engine.Reply{}, err
	}
	clone := *c.c
	clone.Transport = transport
	clone.Jar = call.Jar
	hc := &clone
	if !call.Follow {
		clone.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		hc = &clone
	}
	const maxBody = 8 << 20
	started := time.Now()
	resp, err := hc.Do(req)
	if err != nil {
		return engine.Reply{Duration: time.Since(started)}, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := readResponse(resp.Body, maxBody)
	elapsed := time.Since(started)
	if err != nil {
		return engine.Reply{Status: resp.StatusCode, Duration: elapsed}, err
	}
	if len(raw) > maxBody {
		return engine.Reply{Status: resp.StatusCode, Header: resp.Header, Duration: elapsed}, fmt.Errorf("response exceeds %d bytes", maxBody)
	}
	return engine.Reply{
		URL:      resp.Request.URL.String(),
		Status:   resp.StatusCode,
		Header:   resp.Header,
		Body:     raw,
		Duration: elapsed,
	}, nil
}

// A reply owns its bytes; only temporary read storage can be reused.
func readResponse(body io.Reader, limit int64) ([]byte, error) {
	scratch := responseBufPool.Get().(*[32768]byte) //nolint:forcetypeassert // This private pool stores only fixed-size response scratch arrays.
	defer responseBufPool.Put(scratch)
	reader := io.LimitReader(body, limit+1)
	n, err := io.ReadFull(reader, scratch[:])
	if err == io.EOF || err == io.ErrUnexpectedEOF {
		return bytes.Clone(scratch[:n]), nil
	}
	if err != nil {
		return nil, err
	}
	return io.ReadAll(io.MultiReader(bytes.NewReader(scratch[:n]), reader))
}
