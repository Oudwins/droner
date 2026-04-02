package eventlog_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	sqliteeventlog "github.com/Oudwins/droner/pkgs/droner/dronerd/events/backend/sqlite3"
	"github.com/Oudwins/droner/pkgs/droner/internals/eventlog"
)

func TestAppendAndLoadStream(t *testing.T) {
	log := newTestLog(t, "sessions")

	first, err := log.Append(context.Background(), eventlog.PendingEvent{
		StreamID: "session/a",
		Type:     "session.queued",
		Payload:  []byte(`{"step":1}`),
	})
	if err != nil {
		t.Fatalf("Append first: %v", err)
	}
	second, err := log.Append(context.Background(), eventlog.PendingEvent{
		StreamID: "session/a",
		Type:     "session.ready",
		Payload:  []byte(`{"step":2}`),
	})
	if err != nil {
		t.Fatalf("Append second: %v", err)
	}
	if first.StreamVersion != 1 || second.StreamVersion != 2 {
		t.Fatalf("unexpected stream versions: %d %d", first.StreamVersion, second.StreamVersion)
	}
	if first.Sequence != 1 || second.Sequence != 2 {
		t.Fatalf("unexpected sequences: %d %d", first.Sequence, second.Sequence)
	}

	events, err := log.LoadStream(context.Background(), "session/a", eventlog.LoadStreamOptions{})
	if err != nil {
		t.Fatalf("LoadStream: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].Type != "session.queued" || events[1].Type != "session.ready" {
		t.Fatalf("unexpected event order: %+v", events)
	}
}

func TestTopicSequencesAreIndependent(t *testing.T) {
	backend := newBackend(t)
	sessions := newLogWithBackend(t, backend, "sessions")
	github := newLogWithBackend(t, backend, "github")

	sessionEvent, err := sessions.Append(context.Background(), eventlog.PendingEvent{
		StreamID: "session/a",
		Type:     "session.queued",
		Payload:  []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("sessions append: %v", err)
	}
	githubEvent, err := github.Append(context.Background(), eventlog.PendingEvent{
		StreamID: "pr/123",
		Type:     "github.pr.merged.observed",
		Payload:  []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("github append: %v", err)
	}

	if sessionEvent.Sequence != 1 {
		t.Fatalf("expected session topic sequence 1, got %d", sessionEvent.Sequence)
	}
	if githubEvent.Sequence != 1 {
		t.Fatalf("expected github topic sequence 1, got %d", githubEvent.Sequence)
	}
	if sessionEvent.Topic != "sessions" || githubEvent.Topic != "github" {
		t.Fatalf("unexpected topics: %q %q", sessionEvent.Topic, githubEvent.Topic)
	}
}

func TestSubscribeDoesNotAdvanceCheckpointOnFailure(t *testing.T) {
	log := newTestLog(t, "sessions")
	_, err := log.Append(context.Background(), eventlog.PendingEvent{
		StreamID: "session/a",
		Type:     "session.queued",
		Payload:  []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("Append: %v", err)
	}

	wantErr := errors.New("boom")
	err = log.Subscribe(context.Background(), eventlog.Subscription{
		ID: "projection",
		Handle: func(context.Context, eventlog.Envelope) error {
			return wantErr
		},
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	seen := 0
	err = log.Subscribe(ctx, eventlog.Subscription{
		ID: "projection",
		Handle: func(context.Context, eventlog.Envelope) error {
			seen++
			cancel()
			return nil
		},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
	if seen != 1 {
		t.Fatalf("expected event to be retried once after failure, saw %d", seen)
	}
}

func TestSubscribersAdvanceIndependently(t *testing.T) {
	log := newTestLog(t, "sessions")
	_, err := log.Append(context.Background(), eventlog.PendingEvent{
		StreamID: "session/a",
		Type:     "session.queued",
		Payload:  []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("Append: %v", err)
	}

	ctxA, cancelA := context.WithCancel(context.Background())
	defer cancelA()
	seenA := 0
	err = log.Subscribe(ctxA, eventlog.Subscription{
		ID: "projection-a",
		Handle: func(context.Context, eventlog.Envelope) error {
			seenA++
			cancelA()
			return nil
		},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("subscriber A expected context cancellation, got %v", err)
	}

	ctxB, cancelB := context.WithCancel(context.Background())
	defer cancelB()
	seenB := 0
	err = log.Subscribe(ctxB, eventlog.Subscription{
		ID: "projection-b",
		Handle: func(context.Context, eventlog.Envelope) error {
			seenB++
			cancelB()
			return nil
		},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("subscriber B expected context cancellation, got %v", err)
	}

	if seenA != 1 || seenB != 1 {
		t.Fatalf("expected independent progress, saw A=%d B=%d", seenA, seenB)
	}
}

func newTestLog(t *testing.T, topic eventlog.Topic) eventlog.EventLog {
	t.Helper()
	return newLogWithBackend(t, newBackend(t), topic)
}

func newLogWithBackend(t *testing.T, backend *sqliteeventlog.Backend, topic eventlog.Topic) eventlog.EventLog {
	t.Helper()
	log, err := eventlog.New(eventlog.Config{Topic: topic}, backend)
	if err != nil {
		t.Fatalf("eventlog.New: %v", err)
	}
	return log
}

func newBackend(t *testing.T) *sqliteeventlog.Backend {
	t.Helper()
	backend, err := sqliteeventlog.New(sqliteeventlog.Config{
		Path:         filepath.Join(t.TempDir(), "eventlog.db"),
		PollInterval: 5 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("sqliteeventlog.New: %v", err)
	}
	t.Cleanup(func() {
		if err := backend.Close(); err != nil {
			t.Fatalf("backend.Close: %v", err)
		}
	})
	return backend
}
