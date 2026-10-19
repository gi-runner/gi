package engine

import (
	"bytes"
	"errors"
	"time"

	"github.com/gi-runner/gi/internal/value"
)

func (e *exec) signal(v any) error {
	if e.opt.Suite == nil {
		return errors.New("signal is not available")
	}
	return e.opt.Suite.Signal(v)
}

func (e *exec) listen(timeout time.Duration, wake <-chan struct{}) (any, error) {
	if e.opt.Suite == nil {
		return nil, errors.New("listen is not available")
	}
	return e.opt.Suite.Listen(e.ctx, timeout, wake)
}

func (e *exec) embed(v any, mime, name string) error {
	data, mime, err := embedBytes(v, mime)
	if err != nil {
		return err
	}
	if len(data) > 4<<20 {
		return errors.New("embed exceeds 4 MiB")
	}
	e.embeds = append(e.embeds, Embed{Name: name, Mime: mime, Data: data})
	return nil
}

func embedBytes(v any, mime string) ([]byte, string, error) {
	switch x := v.(type) {
	case []byte:
		raw := bytes.Clone(x)
		if mime == "" {
			mime = sniff(raw)
		}
		return raw, mime, nil
	case string:
		raw := []byte(x)
		if mime == "" {
			mime = "text/plain"
		}
		return raw, mime, nil
	default:
		raw, err := value.Marshal(v)
		if err != nil {
			return nil, "", err
		}
		if mime == "" {
			mime = "application/json"
		}
		return raw, mime, nil
	}
}

func sniff(raw []byte) string {
	switch {
	case bytes.HasPrefix(raw, []byte{0x89, 'P', 'N', 'G'}):
		return "image/png"
	case bytes.HasPrefix(raw, []byte{0xff, 0xd8, 0xff}):
		return "image/jpeg"
	case bytes.HasPrefix(raw, []byte("GIF8")):
		return "image/gif"
	default:
		return "application/octet-stream"
	}
}
