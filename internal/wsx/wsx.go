package wsx

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/coder/websocket"
	"github.com/gi-runner/gi/internal/engine"
	"github.com/gi-runner/gi/internal/value"
)

type link struct{ c *websocket.Conn }

func Dial(ctx context.Context, spec map[string]any) (engine.Link, error) {
	raw := value.Sprint(spec["url"])
	if raw == "" {
		return engine.Link{}, errors.New("websocket requires url")
	}
	options := &websocket.DialOptions{HTTPHeader: make(http.Header)}
	if headers, exists := spec["headers"]; exists {
		m, ok := value.AsMap(headers)
		if !ok {
			return engine.Link{}, errors.New("websocket headers must be an object")
		}
		for name, val := range m {
			if val == nil {
				continue
			}
			if values, ok := value.AsSlice(val); ok {
				for _, item := range values {
					options.HTTPHeader.Add(name, value.Sprint(item))
				}
			} else {
				options.HTTPHeader.Set(name, value.Sprint(val))
			}
		}
	}
	if protocols, exists := spec["subprotocols"]; exists {
		list, ok := value.AsSlice(protocols)
		if !ok {
			return engine.Link{}, errors.New("websocket subprotocols must be a list of strings")
		}
		for _, item := range list {
			protocol, ok := item.(string)
			if !ok || protocol == "" {
				return engine.Link{}, errors.New("websocket subprotocols must be nonempty strings")
			}
			options.Subprotocols = append(options.Subprotocols, protocol)
		}
	}
	var readLimit int64
	if limit, exists := spec["maxMessageSize"]; exists {
		n, ok := value.Normalize(limit).(int64)
		if !ok || n <= 0 {
			return engine.Link{}, errors.New("websocket maxMessageSize must be a positive integer")
		}
		readLimit = n
	}
	c, response, err := websocket.Dial(ctx, raw, options)
	if response != nil && err != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	if err != nil {
		return engine.Link{}, err
	}
	if readLimit > 0 {
		c.SetReadLimit(readLimit)
	}
	l := &link{c: c}
	return engine.Link{Send: l.Send, Receive: l.Receive, Close: l.Close}, nil
}

func (l *link) Send(ctx context.Context, v any) error {
	if raw, ok := v.([]byte); ok {
		return l.c.Write(ctx, websocket.MessageBinary, raw)
	}
	return l.c.Write(ctx, websocket.MessageText, []byte(value.Sprint(v)))
}

func (l *link) Receive(ctx context.Context) (any, error) {
	kind, b, err := l.c.Read(ctx)
	if err != nil {
		return nil, err
	}
	switch kind {
	case websocket.MessageText:
		return string(b), nil
	case websocket.MessageBinary:
		return b, nil
	default:
		return nil, fmt.Errorf("unsupported websocket message type %d", kind)
	}
}

func (l *link) Close() error {
	return l.c.Close(websocket.StatusNormalClosure, "")
}
