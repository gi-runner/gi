package kafkax

import (
	"context"
	"errors"
	"strings"

	"github.com/gi-runner/gi/internal/engine"
	"github.com/gi-runner/gi/internal/value"
	"github.com/twmb/franz-go/pkg/kgo"
)

type record struct {
	value string
	raw   *kgo.Record
}

type link struct {
	cl      *kgo.Client
	topic   string
	group   bool
	pending []record
	commit  func(context.Context, *kgo.Record) error
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
	offset, err := consumeOffset(spec["offset"])
	if err != nil {
		return engine.Link{}, err
	}
	group := value.Sprint(spec["group"])
	if _, ok := spec["group"]; ok {
		if _, ok := spec["group"].(string); !ok || group == "" {
			return engine.Link{}, errors.New("kafka group must be a nonempty string")
		}
	}
	opts := []kgo.Opt{
		kgo.SeedBrokers(brokers...),
		kgo.DefaultProduceTopic(topic),
		kgo.ConsumeTopics(topic),
		kgo.ConsumeResetOffset(offset),
	}
	if group != "" {
		opts = append(opts, kgo.ConsumerGroup(group), kgo.DisableAutoCommit())
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
	l := &link{cl: cl, topic: topic, group: group != ""}
	l.commit = func(ctx context.Context, rec *kgo.Record) error {
		return cl.CommitRecords(ctx, rec)
	}
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
	if err := l.fill(ctx); err != nil {
		return nil, err
	}
	item := l.pending[0]
	if err := l.ack(ctx, item.raw); err != nil {
		return nil, err
	}
	l.pending[0] = record{}
	l.pending = l.pending[1:]
	return item.value, nil
}

func (l *link) fill(ctx context.Context) error {
	for len(l.pending) == 0 {
		fetches := l.cl.PollFetches(ctx)
		if errs := fetches.Errors(); len(errs) > 0 {
			return errs[0].Err
		}
		fetches.EachRecord(func(r *kgo.Record) {
			l.pending = append(l.pending, record{value: string(r.Value), raw: r})
		})
		if len(l.pending) == 0 && ctx.Err() != nil {
			return ctx.Err()
		}
	}
	return nil
}

func (l *link) ack(ctx context.Context, rec *kgo.Record) error {
	if !l.group || rec == nil {
		return nil
	}
	return l.commit(ctx, rec)
}

func (l *link) Close() error {
	l.cl.Close()
	return nil
}

func consumeOffset(v any) (kgo.Offset, error) {
	if v == nil {
		return kgo.NewOffset().AtStart(), nil
	}
	if s, ok := v.(string); ok {
		switch strings.ToLower(s) {
		case "start", "earliest":
			return kgo.NewOffset().AtStart(), nil
		case "end", "latest":
			return kgo.NewOffset().AtEnd(), nil
		default:
			return kgo.Offset{}, errors.New("kafka offset must be start, end, or an integer")
		}
	}
	n, ok := value.AsInt(v)
	if !ok || n < 0 {
		return kgo.Offset{}, errors.New("kafka offset must be start, end, or an integer")
	}
	return kgo.NewOffset().At(int64(n)), nil
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
