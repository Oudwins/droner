package server

import (
	"testing"
	"time"
)

func TestOAuthStateCreateAndStatus(t *testing.T) {
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	current := base
	originalNow := now
	now = func() time.Time { return current }
	t.Cleanup(func() { now = originalNow })

	store := newOAuthStateStore()
	state, err := store.create("device", 5*time.Second, base.Add(30*time.Second))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	record, ok := store.get(state)
	if !ok {
		t.Fatalf("expected state record")
	}
	if record.interval != 5*time.Second {
		t.Fatalf("expected interval 5s, got %v", record.interval)
	}
	if !record.nextPoll.Equal(base.Add(5 * time.Second)) {
		t.Fatalf("unexpected nextPoll %v", record.nextPoll)
	}

	status, errMsg, exists := store.status(state)
	if !exists {
		t.Fatalf("expected state to exist")
	}
	if status != oauthStatusPending {
		t.Fatalf("expected pending, got %s", status)
	}
	if errMsg != "" {
		t.Fatalf("expected empty error, got %q", errMsg)
	}
}

func TestOAuthStateStatusUnknown(t *testing.T) {
	store := newOAuthStateStore()
	status, errMsg, exists := store.status("missing")
	if exists {
		t.Fatalf("expected state to be missing")
	}
	if status != oauthStatusFailed {
		t.Fatalf("expected failed, got %s", status)
	}
	if errMsg != "unknown_state" {
		t.Fatalf("expected unknown_state, got %q", errMsg)
	}
}

func TestOAuthStateExpires(t *testing.T) {
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	current := base
	originalNow := now
	now = func() time.Time { return current }
	t.Cleanup(func() { now = originalNow })

	store := newOAuthStateStore()
	state, err := store.create("device", 5*time.Second, base.Add(30*time.Second))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	current = base.Add(oauthStateTTL + time.Second)
	status, errMsg, exists := store.status(state)
	if !exists {
		t.Fatalf("expected state to exist")
	}
	if status != oauthStatusFailed {
		t.Fatalf("expected failed, got %s", status)
	}
	if errMsg != "expired" {
		t.Fatalf("expected expired, got %q", errMsg)
	}
}

func TestOAuthStateUpdatePoll(t *testing.T) {
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	current := base
	originalNow := now
	now = func() time.Time { return current }
	t.Cleanup(func() { now = originalNow })

	store := newOAuthStateStore()
	state, err := store.create("device", 5*time.Second, base.Add(30*time.Second))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	current = base.Add(10 * time.Second)
	store.updatePoll(state, 15*time.Second)
	record, ok := store.get(state)
	if !ok {
		t.Fatalf("expected state record")
	}
	if record.interval != 15*time.Second {
		t.Fatalf("expected updated interval, got %v", record.interval)
	}
	if !record.nextPoll.Equal(current.Add(15 * time.Second)) {
		t.Fatalf("unexpected nextPoll %v", record.nextPoll)
	}
}
