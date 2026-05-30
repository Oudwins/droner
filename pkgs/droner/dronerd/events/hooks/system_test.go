package hooks

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Oudwins/droner/pkgs/droner/dronerd/events/eventtypes"
	"github.com/Oudwins/droner/pkgs/droner/internals/eventlog"
)

type memorySessionStore struct {
	ref SessionRef
	err error
}

func (s memorySessionStore) LoadByStreamID(context.Context, string) (SessionRef, error) {
	return s.ref, s.err
}

type memoryEventLog struct {
	appended []eventlog.PendingEvent
}

func (l *memoryEventLog) Append(_ context.Context, evt eventlog.PendingEvent) (eventlog.Envelope, error) {
	l.appended = append(l.appended, evt)
	return eventlog.Envelope{ID: eventlog.EventID("event-" + string(evt.Type)), StreamID: evt.StreamID, Type: evt.Type}, nil
}

func (l *memoryEventLog) LoadStream(context.Context, eventlog.StreamID, eventlog.LoadStreamOptions) ([]eventlog.Envelope, error) {
	return nil, nil
}

func (l *memoryEventLog) Subscribe(context.Context, eventlog.Subscription) error {
	return nil
}

func (l *memoryEventLog) Close() error {
	return nil
}

func TestSessionPRMergedRequestsCompletion(t *testing.T) {
	ctx := context.Background()
	sessionsLog := &memoryEventLog{}
	system := New(sessionsLog, nil, memorySessionStore{ref: SessionRef{Branch: "feature", LifecycleState: "session.ready"}}, nil)

	if err := system.handleSessionEvent(ctx, eventlog.Envelope{
		ID:       eventlog.EventID("cause-1"),
		StreamID: eventlog.StreamID("session-1"),
		Type:     eventtypes.SessionPRMerged,
	}); err != nil {
		t.Fatalf("handleSessionEvent: %v", err)
	}

	if len(sessionsLog.appended) != 1 {
		t.Fatalf("expected one appended event, got %d", len(sessionsLog.appended))
	}
	evt := sessionsLog.appended[0]
	if evt.StreamID != "session-1" || evt.Type != eventtypes.SessionCompletionRequested {
		t.Fatalf("unexpected event: %#v", evt)
	}
	if evt.CausationID != "cause-1" || evt.CorrelationID != "session-1" {
		t.Fatalf("unexpected causation/correlation: %#v", evt)
	}

	var payload branchPayload
	if err := json.Unmarshal(evt.Payload, &payload); err != nil {
		t.Fatalf("Unmarshal payload: %v", err)
	}
	if payload.Branch != "feature" {
		t.Fatalf("payload branch = %q, want feature", payload.Branch)
	}
}

func TestSessionPRMergedSkipsCompletionWhenAlreadyRequested(t *testing.T) {
	states := []eventlog.EventType{
		eventtypes.SessionCompletionRequested,
		eventtypes.SessionCompletionStarted,
		eventtypes.SessionCompletionSuccess,
		eventtypes.SessionCompletionFailed,
		eventtypes.SessionDeletionRequested,
		eventtypes.SessionDeletionStarted,
		eventtypes.SessionDeletionSuccess,
		eventtypes.SessionDeletionFailed,
	}

	for _, state := range states {
		t.Run(string(state), func(t *testing.T) {
			sessionsLog := &memoryEventLog{}
			system := New(sessionsLog, nil, memorySessionStore{ref: SessionRef{Branch: "feature", LifecycleState: string(state)}}, nil)

			if err := system.handleSessionEvent(context.Background(), eventlog.Envelope{
				ID:       eventlog.EventID("cause-1"),
				StreamID: eventlog.StreamID("session-1"),
				Type:     eventtypes.SessionPRMerged,
			}); err != nil {
				t.Fatalf("handleSessionEvent: %v", err)
			}
			if len(sessionsLog.appended) != 0 {
				t.Fatalf("expected no appended events, got %d", len(sessionsLog.appended))
			}
		})
	}
}
