package eventlog

import (
	"context"
	"strings"
)

type backend interface {
	Append(ctx context.Context, topic Topic, evt PendingEvent) (Envelope, error)
	LoadStream(ctx context.Context, topic Topic, streamID StreamID, opts LoadStreamOptions) ([]Envelope, error)
	ReadGlobal(ctx context.Context, topic Topic, afterSequence int64, limit int) ([]Envelope, error)
	LoadCheckpoint(ctx context.Context, topic Topic, subscriber SubscriberID) (int64, error)
	StoreCheckpoint(ctx context.Context, topic Topic, subscriber SubscriberID, sequence int64) error
	Close() error
}

type log struct {
	topic   Topic
	backend backend
}

func New(cfg Config, b backend) (EventLog, error) {
	if strings.TrimSpace(string(cfg.Topic)) == "" {
		return nil, ErrTopicRequired
	}
	if b == nil {
		return nil, ErrBackendRequired
	}
	return &log{topic: cfg.Topic, backend: b}, nil
}

func (l *log) Append(ctx context.Context, evt PendingEvent) (Envelope, error) {
	if strings.TrimSpace(string(evt.StreamID)) == "" {
		return Envelope{}, ErrStreamIDRequired
	}
	if strings.TrimSpace(string(evt.Type)) == "" {
		return Envelope{}, ErrEventTypeRequired
	}
	if evt.SchemaVersion <= 0 {
		evt.SchemaVersion = 1
	}
	if evt.Payload == nil {
		evt.Payload = []byte("null")
	}
	return l.backend.Append(ctx, l.topic, evt)
}

func (l *log) LoadStream(ctx context.Context, streamID StreamID, opts LoadStreamOptions) ([]Envelope, error) {
	if strings.TrimSpace(string(streamID)) == "" {
		return nil, ErrStreamIDRequired
	}
	return l.backend.LoadStream(ctx, l.topic, streamID, opts)
}

func (l *log) Close() error {
	return l.backend.Close()
}
