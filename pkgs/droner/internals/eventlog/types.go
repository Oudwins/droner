package eventlog

import (
	"context"
	"time"
)

type Topic string
type StreamID string
type EventID string
type EventType string
type SubscriberID string

type Envelope struct {
	ID            EventID
	Topic         Topic
	StreamID      StreamID
	StreamVersion int64
	Sequence      int64
	Type          EventType
	SchemaVersion int
	OccurredAt    time.Time
	CausationID   EventID
	CorrelationID string
	Payload       []byte
}

type PendingEvent struct {
	StreamID      StreamID
	Type          EventType
	SchemaVersion int
	Payload       []byte
	CausationID   EventID
	CorrelationID string
}

type Config struct {
	Topic Topic
}

type LoadStreamOptions struct {
	AfterVersion int64
	Limit        int
}

type Subscription struct {
	ID     SubscriberID
	Filter func(Envelope) bool
	Handle func(context.Context, Envelope) error
}

type EventLog interface {
	Append(ctx context.Context, evt PendingEvent) (Envelope, error)
	LoadStream(ctx context.Context, streamID StreamID, opts LoadStreamOptions) ([]Envelope, error)
	Subscribe(ctx context.Context, sub Subscription) error
	Close() error
}
