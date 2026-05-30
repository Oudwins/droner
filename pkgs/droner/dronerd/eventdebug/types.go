package eventdebug

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

var ErrStreamNotFound = errors.New("event stream not found")

const (
	topicAll          = "all"
	topicSessions     = "sessions"
	topicPullRequests = "pullrequests"
)

type StreamSummary struct {
	Topic           string    `json:"topic"`
	StreamID        string    `json:"streamId"`
	EventCount      int       `json:"eventCount"`
	FirstOccurredAt time.Time `json:"firstOccurredAt"`
	LastOccurredAt  time.Time `json:"lastOccurredAt"`
}

type Event struct {
	ID            string          `json:"id"`
	Topic         string          `json:"topic"`
	StreamID      string          `json:"streamId"`
	StreamVersion int64           `json:"streamVersion"`
	EventType     string          `json:"eventType"`
	SchemaVersion int             `json:"schemaVersion"`
	OccurredAt    time.Time       `json:"occurredAt"`
	CausationID   string          `json:"causationId,omitempty"`
	CorrelationID string          `json:"correlationId,omitempty"`
	Payload       json.RawMessage `json:"payload"`
}

type Stream struct {
	Topic   string        `json:"topic"`
	Summary StreamSummary `json:"summary"`
	Events  []Event       `json:"events"`
}

type ListOptions struct {
	Topics []string
	Query  string
	Limit  int
}

type StreamOptions struct {
	Topic string
	Limit int
}

type Store interface {
	ListStreams(ctx context.Context, opts ListOptions) ([]StreamSummary, error)
	LoadStream(ctx context.Context, streamID string, opts StreamOptions) (Stream, error)
}
