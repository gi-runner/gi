package wsx

import (
	"context"
	"errors"

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
	c, _, err := websocket.Dial(ctx, raw, nil)
	if err != nil {
		return engine.Link{}, err
	}
	l := &link{c: c}
	return engine.Link{Send: l.Send, Receive: l.Receive, Close: l.Close}, nil
}

func (l *link) Send(ctx context.Context, v any) error {
	return l.c.Write(ctx, websocket.MessageText, []byte(value.Sprint(v)))
}

func (l *link) Receive(ctx context.Context) (any, error) {
	_, b, err := l.c.Read(ctx)
	if err != nil {
		return nil, err
	}
	return string(b), nil
}

func (l *link) Close() error {
	return l.c.Close(websocket.StatusNormalClosure, "")
}
