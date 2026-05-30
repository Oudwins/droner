package hooks

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/Oudwins/droner/pkgs/droner/dronerd/events/eventtypes"
	"github.com/Oudwins/droner/pkgs/droner/internals/eventlog"
)

const (
	consumerHooks = "hooks"
)

type System struct {
	sessionsLog eventlog.EventLog
	prLog       eventlog.EventLog
	sessions    SessionLookupStore
	logger      *slog.Logger
	startOnce   sync.Once
}

type branchPayload struct {
	Branch string `json:"branch"`
}

func New(sessionsLog eventlog.EventLog, prLog eventlog.EventLog, sessions SessionLookupStore, logger *slog.Logger) *System {
	return &System{sessionsLog: sessionsLog, prLog: prLog, sessions: sessions, logger: logger}
}

func (s *System) Start(ctx context.Context) {
	if s == nil {
		return
	}
	s.startOnce.Do(func() {
		go s.runSessionSubscription(ctx)
		go s.runPullRequestSubscription(ctx)
	})
}

func (s *System) Close() error {
	return nil
}

func (s *System) runSessionSubscription(ctx context.Context) {
	s.runSubscription(ctx, s.sessionsLog, eventlog.Subscription{
		ID: eventlog.SubscriberID(consumerHooks),
		Filter: func(evt eventlog.Envelope) bool {
			return evt.Type == eventtypes.SessionPRMerged
		},
		Handle: s.handleSessionEvent,
	})
}

func (s *System) runPullRequestSubscription(ctx context.Context) {
	s.runSubscription(ctx, s.prLog, eventlog.Subscription{
		ID: eventlog.SubscriberID(consumerHooks),
		Handle: func(context.Context, eventlog.Envelope) error {
			return nil
		},
	})
}

func (s *System) runSubscription(ctx context.Context, log eventlog.EventLog, sub eventlog.Subscription) {
	if log == nil {
		return
	}
	for {
		if err := log.Subscribe(ctx, sub); err != nil && !errors.Is(err, context.Canceled) {
			if s.logger != nil {
				s.logger.Error("hooks subscription failed", "subscriber_id", sub.ID, "error", err)
			}
		} else {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func (s *System) handleSessionEvent(ctx context.Context, evt eventlog.Envelope) error {
	switch evt.Type {
	case eventtypes.SessionPRMerged:
		return s.completeSession(ctx, evt)
	default:
		return nil
	}
}

func (s *System) completeSession(ctx context.Context, cause eventlog.Envelope) error {
	ref, err := s.sessions.LoadByStreamID(ctx, string(cause.StreamID))
	if err != nil {
		return err
	}
	if completionAlreadyRequested(ref.LifecycleState) {
		return nil
	}
	return s.appendSessionEvent(ctx, string(cause.StreamID), eventtypes.SessionCompletionRequested, branchPayload{Branch: ref.Branch}, string(cause.ID), string(cause.StreamID))
}

func completionAlreadyRequested(lifecycleState string) bool {
	switch eventlog.EventType(lifecycleState) {
	case eventtypes.SessionCompletionRequested,
		eventtypes.SessionCompletionStarted,
		eventtypes.SessionCompletionSuccess,
		eventtypes.SessionCompletionFailed,
		eventtypes.SessionDeletionRequested,
		eventtypes.SessionDeletionStarted,
		eventtypes.SessionDeletionSuccess,
		eventtypes.SessionDeletionFailed:
		return true
	default:
		return false
	}
}

func (s *System) appendSessionEvent(ctx context.Context, streamID string, eventType eventlog.EventType, payload any, causationID, correlationID string) error {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = s.sessionsLog.Append(ctx, eventlog.PendingEvent{
		StreamID:      eventlog.StreamID(streamID),
		Type:          eventType,
		SchemaVersion: 1,
		Payload:       payloadBytes,
		CausationID:   eventlog.EventID(causationID),
		CorrelationID: correlationID,
	})
	return err
}
