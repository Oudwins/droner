package eventdebug

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type memoryStore struct {
	streams []StreamSummary
	byID    map[string]Stream
}

func (m memoryStore) ListStreams(_ context.Context, opts ListOptions) ([]StreamSummary, error) {
	if strings.TrimSpace(opts.Query) == "" {
		return m.streams, nil
	}
	filtered := make([]StreamSummary, 0)
	for _, stream := range m.streams {
		if strings.Contains(stream.StreamID, opts.Query) {
			filtered = append(filtered, stream)
		}
	}
	return filtered, nil
}

func (m memoryStore) LoadStream(_ context.Context, streamID string, _ StreamOptions) (Stream, error) {
	stream, ok := m.byID[streamID]
	if !ok {
		return Stream{}, ErrStreamNotFound
	}
	return stream, nil
}

func TestServerListAPI(t *testing.T) {
	now := time.Date(2026, 3, 29, 12, 0, 0, 0, time.UTC)
	server := NewServer(memoryStore{
		streams: []StreamSummary{{StreamID: "session/a", EventCount: 2, FirstOccurredAt: now, LastOccurredAt: now}},
		byID:    map[string]Stream{},
	}, ServerOptions{})

	req := httptest.NewRequest(http.MethodGet, "/api/streams", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var payload struct {
		Streams []StreamSummary `json:"streams"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(payload.Streams) != 1 || payload.Streams[0].StreamID != "session/a" {
		t.Fatalf("unexpected streams payload: %#v", payload.Streams)
	}
}

func TestServerStreamAPI(t *testing.T) {
	now := time.Date(2026, 3, 29, 12, 0, 0, 0, time.UTC)
	server := NewServer(memoryStore{
		streams: nil,
		byID: map[string]Stream{
			"session/a": {
				Summary: StreamSummary{StreamID: "session/a", EventCount: 1, FirstOccurredAt: now, LastOccurredAt: now},
				Events:  []Event{{ID: "evt-1", StreamID: "session/a", StreamVersion: 1, EventType: "session.queued", SchemaVersion: 1, OccurredAt: now, Payload: json.RawMessage(`{"ok":true}`)}},
			},
		},
	}, ServerOptions{})

	req := httptest.NewRequest(http.MethodGet, "/api/streams/session%2Fa", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var payload Stream
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if payload.Summary.StreamID != "session/a" {
		t.Fatalf("stream id = %q, want session/a", payload.Summary.StreamID)
	}
	if len(payload.Events) != 1 || payload.Events[0].EventType != "session.queued" {
		t.Fatalf("unexpected events: %#v", payload.Events)
	}
}

func TestServerRendersHTMLPage(t *testing.T) {
	now := time.Date(2026, 3, 29, 12, 0, 0, 0, time.UTC)
	server := NewServer(memoryStore{
		streams: []StreamSummary{{StreamID: "session/a", EventCount: 1, FirstOccurredAt: now, LastOccurredAt: now}},
		byID: map[string]Stream{
			"session/a": {
				Summary: StreamSummary{StreamID: "session/a", EventCount: 3, FirstOccurredAt: now, LastOccurredAt: now.Add(1800 * time.Millisecond)},
				Events: []Event{
					{ID: "evt-1", StreamID: "session/a", StreamVersion: 1, EventType: "session.environment_provisioning.started", SchemaVersion: 1, OccurredAt: now, Payload: json.RawMessage(`{"path":"/tmp/repo"}`)},
					{ID: "evt-2", StreamID: "session/a", StreamVersion: 2, EventType: "session.environment_provisioning.success", SchemaVersion: 1, OccurredAt: now.Add(1200 * time.Millisecond), Payload: json.RawMessage(`{"path":"/tmp/repo"}`)},
					{ID: "evt-3", StreamID: "session/a", StreamVersion: 3, EventType: "session.environment_provisioning.failed", SchemaVersion: 1, OccurredAt: now.Add(1800 * time.Millisecond), Payload: json.RawMessage(`{"path":"/tmp/repo"}`)},
				},
			},
		},
	}, ServerOptions{})

	req := httptest.NewRequest(http.MethodGet, "/?stream=session%2Fa", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "session/a") || !strings.Contains(body, "environment_provisioning") {
		t.Fatalf("expected rendered stream details, body=%s", body)
	}
	if !strings.Contains(body, "3 total events. 1800ms total. Showing up to 500.") {
		t.Fatalf("expected total stream duration in hero, body=%s", body)
	}
	if !strings.Contains(body, "started / success / failed") {
		t.Fatalf("expected grouped status summary in HTML, body=%s", body)
	}
	if !strings.Contains(body, "exec 1800ms") {
		t.Fatalf("expected group exec time in HTML, body=%s", body)
	}
	if !strings.Contains(body, `<details class="event-group">`) {
		t.Fatalf("expected collapsible event group, body=%s", body)
	}
	if !strings.Contains(body, "took 1200ms") {
		t.Fatalf("expected rounded elapsed time in header, body=%s", body)
	}
}

func TestFmtElapsedSincePrevious(t *testing.T) {
	events := []Event{
		{OccurredAt: time.Date(2026, 3, 29, 12, 0, 0, 0, time.UTC)},
		{OccurredAt: time.Date(2026, 3, 29, 12, 0, 1, 200000000, time.UTC)},
	}

	if got := fmtElapsedSincePrevious(0, events); got != "" {
		t.Fatalf("first event delta = %q, want empty string", got)
	}
	if got := fmtElapsedSincePrevious(1, events); got != "took 1200ms" {
		t.Fatalf("second event delta = %q, want %q", got, "took 1200ms")
	}
	if got := fmtElapsedBetween(events[0].OccurredAt, events[1].OccurredAt); got != "1200ms" {
		t.Fatalf("stream duration = %q, want %q", got, "1200ms")
	}
}

func TestBuildEventGroups(t *testing.T) {
	now := time.Date(2026, 3, 29, 12, 0, 0, 0, time.UTC)
	events := []Event{
		{EventType: "session.environment_provisioning.started", OccurredAt: now},
		{EventType: "session.environment_provisioning.success", OccurredAt: now.Add(1200 * time.Millisecond)},
		{EventType: "session.execution.started", OccurredAt: now.Add(2 * time.Second)},
	}

	groups := buildEventGroups(events)
	if len(groups) != 2 {
		t.Fatalf("group count = %d, want 2", len(groups))
	}
	if groups[0].Action != "environment_provisioning" {
		t.Fatalf("first group action = %q, want %q", groups[0].Action, "environment_provisioning")
	}
	if got := strings.Join(groups[0].Statuses, "/"); got != "started/success" {
		t.Fatalf("first group statuses = %q, want %q", got, "started/success")
	}
	if groups[0].ExecTime != "1200ms" {
		t.Fatalf("first group exec time = %q, want %q", groups[0].ExecTime, "1200ms")
	}
	if groups[1].Action != "execution" {
		t.Fatalf("second group action = %q, want %q", groups[1].Action, "execution")
	}
	if groups[1].IdleTime != "800ms" {
		t.Fatalf("second group idle time = %q, want %q", groups[1].IdleTime, "800ms")
	}
}

func TestBuildEventGroupsLeavesTwoPartEventsStandalone(t *testing.T) {
	now := time.Date(2026, 3, 29, 12, 0, 0, 0, time.UTC)
	events := []Event{
		{EventType: "session.queued", OccurredAt: now},
		{EventType: "session.runtime_started", OccurredAt: now.Add(1200 * time.Millisecond)},
	}

	groups := buildEventGroups(events)
	if len(groups) != 2 {
		t.Fatalf("group count = %d, want 2", len(groups))
	}
	if groups[0].Action != "session.queued" {
		t.Fatalf("first group action = %q, want %q", groups[0].Action, "session.queued")
	}
	if groups[1].Action != "session.runtime_started" {
		t.Fatalf("second group action = %q, want %q", groups[1].Action, "session.runtime_started")
	}
}
