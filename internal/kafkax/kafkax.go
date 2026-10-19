package kafkax

import (
	"context"
	"errors"
	"strings"

	"github.com/gi-runner/gi/internal/engine"
	"github.com/gi-runner/gi/internal/value"
	"github.com/twmb/franz-go/pkg/kgo"
)

type link struct {
	cl      *kgo.Client
	topic   string
	pending []string
}

func Dial(ctx context.Context, spec map[string]any) (engine.Link, error) {
	if err := ctx.Err(); err != nil {
		return engine.Link{}, err
	}
	brokers := hosts(spec["brokers"])
	topic := value.Sprint(spec["topic"])
	if len(brokers) == 0 || topic == "" {
		return engine.Link{}, errors.New("kafka requires brokers and topic")
	}
	mech, err := saslMechanism(spec["sasl"])
	if err != nil {
		return engine.Link{}, err
	}
	tlsCfg, err := kafkaTLS(spec["tls"])
	if err != nil {
		return engine.Link{}, err
	}
	opts := []kgo.Opt{
		kgo.SeedBrokers(brokers...),
		kgo.DefaultProduceTopic(topic),
		kgo.ConsumeTopics(topic),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
	}
	if mech != nil {
		opts = append(opts, kgo.SASL(mech))
	}
	if tlsCfg != nil {
		opts = append(opts, kgo.DialTLSConfig(tlsCfg))
	}
	cl, err := kgo.NewClient(opts...)
	if err != nil {
		return engine.Link{}, err
	}
	l := &link{cl: cl, topic: topic}
	return engine.Link{Send: l.Send, Receive: l.Receive, Close: l.Close}, nil
}

func (l *link) Send(ctx context.Context, v any) error {
	body := []byte(value.Sprint(v))
	if _, ok := v.(string); !ok && v != nil {
		raw, err := value.Marshal(v)
		if err != nil {
			return err
		}
		body = raw
	}
	return l.cl.ProduceSync(ctx, &kgo.Record{Topic: l.topic, Value: body}).FirstErr()
}

func (l *link) Receive(ctx context.Context) (any, error) {
	if len(l.pending) > 0 {
		v := l.pending[0]
		l.pending = l.pending[1:]
		return v, nil
	}
	for {
		fetches := l.cl.PollFetches(ctx)
		if errs := fetches.Errors(); len(errs) > 0 {
			return nil, errs[0].Err
		}
		var batch []string
		fetches.EachRecord(func(r *kgo.Record) {
			batch = append(batch, string(r.Value))
		})
		if len(batch) > 0 {
			l.pending = batch[1:]
			return batch[0], nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
}

func (l *link) Close() error {
	l.cl.Close()
	return nil
}

func hosts(v any) []string {
	if s, ok := v.(string); ok {
		var out []string
		for part := range strings.SplitSeq(s, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				out = append(out, part)
			}
		}
		return out
	}
	items, ok := value.AsSlice(v)
	if !ok {
		if v == nil {
			return nil
		}
		return []string{value.Sprint(v)}
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, value.Sprint(item))
	}
	return out
}
