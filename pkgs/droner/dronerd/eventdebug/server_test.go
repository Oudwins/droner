package eventdebug

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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
				Summary: StreamSummary{StreamID: "session/a", EventCount: 4, FirstOccurredAt: now, LastOccurredAt: now.Add(2500 * time.Millisecond)},
				Events: []Event{
					{ID: "evt-1", StreamID: "session/a", StreamVersion: 1, EventType: "session.environment_provisioning.started", SchemaVersion: 1, OccurredAt: now, Payload: json.RawMessage(`{"path":"/tmp/repo"}`)},
					{ID: "evt-2", StreamID: "session/a", StreamVersion: 2, EventType: "session.environment_provisioning.success", SchemaVersion: 1, OccurredAt: now.Add(1200 * time.Millisecond), Payload: json.RawMessage(`{"path":"/tmp/repo"}`)},
					{ID: "evt-3", StreamID: "session/a", StreamVersion: 3, EventType: "session.environment_provisioning.failed", SchemaVersion: 1, OccurredAt: now.Add(1800 * time.Millisecond), Payload: json.RawMessage(`{"path":"/tmp/repo"}`)},
					{ID: "evt-4", StreamID: "session/a", StreamVersion: 4, EventType: "session.execution.started", SchemaVersion: 1, OccurredAt: now.Add(2500 * time.Millisecond), Payload: json.RawMessage(`{"path":"/tmp/repo"}`)},
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
	if !strings.Contains(body, "4 total events. 2500ms total. Showing up to 500.") {
		t.Fatalf("expected total stream duration in hero, body=%s", body)
	}
	if !strings.Contains(body, "started / success / failed") {
		t.Fatalf("expected grouped status summary in HTML, body=%s", body)
	}
	if !strings.Contains(body, "v1-3 environment_provisioning") {
		t.Fatalf("expected grouped version range in HTML, body=%s", body)
	}
	if !strings.Contains(body, "exec 1800ms") {
		t.Fatalf("expected group exec time in HTML, body=%s", body)
	}
	if count := strings.Count(body, `<details class="event-group">`); count != 1 {
		t.Fatalf("details count = %d, want 1, body=%s", count, body)
	}
	if !strings.Contains(body, "v4 session.execution.started") {
		t.Fatalf("expected single event to render as a normal card, body=%s", body)
	}
	if !strings.Contains(body, "took 1200ms") {
		t.Fatalf("expected rounded elapsed time in header, body=%s", body)
	}
	if !strings.Contains(body, "Reset To Here") {
		t.Fatalf("expected reset button to render, body=%s", body)
	}
}

func TestServerResetProxyForwardsToMainServer(t *testing.T) {
	var (
		called  bool
		gotBody []byte
	)
	mainServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/sessions/reset" {
			t.Fatalf("path = %q, want /sessions/reset", r.URL.Path)
		}
		var err error
		gotBody, err = io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll: %v", err)
		}
		called = true
		w.WriteHeader(http.StatusAccepted)
	}))
	defer mainServer.Close()

	server := NewServer(memoryStore{}, ServerOptions{MainServerURL: mainServer.URL})
	form := url.Values{
		"stream_id":    {"session/a"},
		"event_id":     {"evt-1"},
		"q":            {"session"},
		"limit":        {"12"},
		"stream_limit": {"34"},
	}
	req := httptest.NewRequest(http.MethodPost, "/reset", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if !called {
		t.Fatal("expected reset proxy to call main server")
	}
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}
	if got, want := rec.Header().Get("Location"), "/?limit=12&q=session&stream=session%2Fa&stream_limit=34"; got != want {
		t.Fatalf("redirect location = %q, want %q", got, want)
	}
	if got := string(gotBody); got != `{"eventId":"evt-1","streamId":"session/a"}` {
		t.Fatalf("forwarded body = %s", got)
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
		{StreamVersion: 2, EventType: "session.environment_provisioning.started", OccurredAt: now},
		{StreamVersion: 3, EventType: "session.environment_provisioning.success", OccurredAt: now.Add(1200 * time.Millisecond)},
		{StreamVersion: 4, EventType: "session.execution.started", OccurredAt: now.Add(2 * time.Second)},
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
	if groups[0].VersionLabel != "v2-3" {
		t.Fatalf("first group version label = %q, want %q", groups[0].VersionLabel, "v2-3")
	}
	if groups[1].Action != "execution" {
		t.Fatalf("second group action = %q, want %q", groups[1].Action, "execution")
	}
	if groups[1].VersionLabel != "v4" {
		t.Fatalf("second group version label = %q, want %q", groups[1].VersionLabel, "v4")
	}
	if groups[1].IdleTime != "800ms" {
		t.Fatalf("second group idle time = %q, want %q", groups[1].IdleTime, "800ms")
	}
}

func TestBuildEventGroupsLeavesTwoPartEventsStandalone(t *testing.T) {
	now := time.Date(2026, 3, 29, 12, 0, 0, 0, time.UTC)
	events := []Event{
		{EventType: "session.queued", OccurredAt: now},
		{EventType: "session.ready", OccurredAt: now.Add(1200 * time.Millisecond)},
	}

	groups := buildEventGroups(events)
	if len(groups) != 2 {
		t.Fatalf("group count = %d, want 2", len(groups))
	}
	if groups[0].Action != "session.queued" {
		t.Fatalf("first group action = %q, want %q", groups[0].Action, "session.queued")
	}
	if groups[1].Action != "session.ready" {
		t.Fatalf("second group action = %q, want %q", groups[1].Action, "session.ready")
	}
}
