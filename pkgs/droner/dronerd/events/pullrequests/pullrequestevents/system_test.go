package pullrequestevents

import (
	"context"
	"testing"
	"time"

	coredb "github.com/Oudwins/droner/pkgs/droner/dronerd/db"
	"github.com/Oudwins/droner/pkgs/droner/dronerd/events/eventlogs"
	"github.com/Oudwins/droner/pkgs/droner/dronerd/events/eventtypes"
	"github.com/Oudwins/droner/pkgs/droner/dronerd/internals/remote"
	"github.com/Oudwins/droner/pkgs/droner/internals/eventlog"
)

func TestIngestObservedWritesInitialSnapshotAndEvents(t *testing.T) {
	system := newTestSystem(t)
	ctx := context.Background()
	observedAt := time.Date(2026, time.April, 22, 12, 0, 0, 0, time.UTC)
	snapshot := testSnapshot("pending")

	if err := system.ingestObserved(ctx, "session-1", snapshot, observedAt); err != nil {
		t.Fatalf("ingestObserved: %v", err)
	}

	stored, found, err := system.snapshots.Load(ctx, "github:owner/repo#42")
	if err != nil {
		t.Fatalf("Load snapshot: %v", err)
	}
	if !found || stored.Number != 42 || stored.HeadRef != "feature" || stored.HeadSHA != "abc" {
		t.Fatalf("unexpected snapshot: %#v", stored)
	}

	prEvents, err := system.prLog.LoadStream(ctx, eventlog.StreamID("github:owner/repo#42"), eventlog.LoadStreamOptions{})
	if err != nil {
		t.Fatalf("LoadStream pr: %v", err)
	}
	if len(prEvents) != 1 || prEvents[0].Type != eventtypes.PRObserved {
		t.Fatalf("unexpected PR events: %#v", prEvents)
	}

	sessionEvents, err := system.sessionLog.LoadStream(ctx, eventlog.StreamID("session-1"), eventlog.LoadStreamOptions{})
	if err != nil {
		t.Fatalf("LoadStream session: %v", err)
	}
	want := []eventlog.EventType{eventtypes.SessionPRLinked, eventtypes.SessionPRStateChanged, eventtypes.SessionPRCIStateChanged}
	if len(sessionEvents) != len(want) {
		t.Fatalf("expected %d session events, got %d", len(want), len(sessionEvents))
	}
	for i, eventType := range want {
		if sessionEvents[i].Type != eventType {
			t.Fatalf("event %d = %s, want %s", i, sessionEvents[i].Type, eventType)
		}
	}
}

func TestIngestObservedSkipsUnchangedSnapshot(t *testing.T) {
	system := newTestSystem(t)
	ctx := context.Background()
	snapshot := testSnapshot("passing")

	if err := system.ingestObserved(ctx, "session-1", snapshot, time.Now().UTC()); err != nil {
		t.Fatalf("first ingestObserved: %v", err)
	}
	if err := system.ingestObserved(ctx, "session-1", snapshot, time.Now().UTC()); err != nil {
		t.Fatalf("second ingestObserved: %v", err)
	}

	prEvents, err := system.prLog.LoadStream(ctx, eventlog.StreamID("github:owner/repo#42"), eventlog.LoadStreamOptions{})
	if err != nil {
		t.Fatalf("LoadStream pr: %v", err)
	}
	if len(prEvents) != 1 {
		t.Fatalf("expected one PR event, got %d", len(prEvents))
	}
}

func TestIngestObservedWritesDeltaAndNamedCIEvent(t *testing.T) {
	system := newTestSystem(t)
	ctx := context.Background()
	first := testSnapshot("pending")
	second := testSnapshot("failing")

	if err := system.ingestObserved(ctx, "session-1", first, time.Now().UTC()); err != nil {
		t.Fatalf("first ingestObserved: %v", err)
	}
	if err := system.ingestObserved(ctx, "session-1", second, time.Now().UTC()); err != nil {
		t.Fatalf("second ingestObserved: %v", err)
	}

	prEvents, err := system.prLog.LoadStream(ctx, eventlog.StreamID("github:owner/repo#42"), eventlog.LoadStreamOptions{})
	if err != nil {
		t.Fatalf("LoadStream pr: %v", err)
	}
	if len(prEvents) != 2 || prEvents[1].Type != eventtypes.PRObserved {
		t.Fatalf("unexpected PR events: %#v", prEvents)
	}

	sessionEvents, err := system.sessionLog.LoadStream(ctx, eventlog.StreamID("session-1"), eventlog.LoadStreamOptions{})
	if err != nil {
		t.Fatalf("LoadStream session: %v", err)
	}
	last := sessionEvents[len(sessionEvents)-1]
	if last.Type != eventtypes.SessionPRCIStateChanged {
		t.Fatalf("last session event = %s, want %s", last.Type, eventtypes.SessionPRCIStateChanged)
	}
}

func newTestSystem(t *testing.T) *System {
	t.Helper()
	dataDir := t.TempDir()
	conn, err := coredb.OpenSQLiteDB(coredb.DBPath(dataDir))
	if err != nil {
		t.Fatalf("OpenSQLiteDB: %v", err)
	}
	logs, err := eventlogs.Open(dataDir)
	if err != nil {
		_ = conn.Close()
		t.Fatalf("eventlogs.Open: %v", err)
	}
	prLog, err := logs.PullRequests()
	if err != nil {
		_ = logs.Close()
		_ = conn.Close()
		t.Fatalf("PullRequests log: %v", err)
	}
	sessionLog, err := logs.Sessions()
	if err != nil {
		_ = logs.Close()
		_ = conn.Close()
		t.Fatalf("Sessions log: %v", err)
	}
	queries := coredb.New(conn)
	system := New(prLog, sessionLog, NewSQLiteSessionLookupStore(queries), NewSQLitePullRequestSnapshotStore(queries), nil)
	t.Cleanup(func() {
		_ = system.Close()
		_ = logs.Close()
		_ = conn.Close()
	})
	return system
}

func testSnapshot(ciState string) remote.PullRequestSnapshot {
	return remote.PullRequestSnapshot{
		Provider:           "github",
		RemoteURL:          "git@github.com:owner/repo.git",
		RepoOwner:          "owner",
		RepoName:           "repo",
		Number:             42,
		State:              "open",
		Title:              "Ship it",
		HTMLURL:            "https://github.com/owner/repo/pull/42",
		HeadRef:            "feature",
		HeadSHA:            "abc",
		BaseRef:            "main",
		RequestedReviewers: []string{"alice"},
		CI:                 remote.CIStatusSummary{State: ciState},
		CreatedAt:          time.Date(2026, time.April, 22, 10, 0, 0, 0, time.UTC),
		UpdatedAt:          time.Date(2026, time.April, 22, 11, 0, 0, 0, time.UTC),
	}
}
