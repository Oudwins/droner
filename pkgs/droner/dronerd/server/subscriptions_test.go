package server

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/Oudwins/droner/pkgs/droner/internals/remote"
)

func TestSubscriptionManagerIdempotentSubscribe(t *testing.T) {
	manager := newSubscriptionManager()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	ctx := context.Background()

	originalSubscribe := subscribeRemote
	originalUnsubscribe := unsubscribeRemote
	defer func() {
		subscribeRemote = originalSubscribe
		unsubscribeRemote = originalUnsubscribe
	}()

	callCount := 0
	subscribeRemote = func(ctx context.Context, remoteURL string, branch string, handler remote.BranchEventHandler) error {
		callCount++
		return nil
	}
	unsubscribeRemote = func(ctx context.Context, remoteURL string, branch string) error { return nil }

	if err := manager.subscribe(ctx, "git@github.com:org/repo.git", "abc", logger, func(string) {}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if err := manager.subscribe(ctx, "git@github.com:org/repo.git", "abc", logger, func(string) {}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if callCount != 1 {
		t.Fatalf("expected subscribe called once, got %d", callCount)
	}
}

func TestSubscriptionManagerUnsubscribeNoop(t *testing.T) {
	manager := newSubscriptionManager()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	ctx := context.Background()

	originalSubscribe := subscribeRemote
	originalUnsubscribe := unsubscribeRemote
	defer func() {
		subscribeRemote = originalSubscribe
		unsubscribeRemote = originalUnsubscribe
	}()

	unsubscribeCalled := false
	subscribeRemote = func(ctx context.Context, remoteURL string, branch string, handler remote.BranchEventHandler) error {
		return nil
	}
	unsubscribeRemote = func(ctx context.Context, remoteURL string, branch string) error {
		unsubscribeCalled = true
		return nil
	}

	if err := manager.unsubscribe(ctx, "git@github.com:org/repo.git", "missing", logger); err != nil {
		t.Fatalf("unsubscribe: %v", err)
	}
	if unsubscribeCalled {
		t.Fatalf("expected unsubscribe not called for missing subscription")
	}
}

func TestSubscriptionManagerCleanupEvents(t *testing.T) {
	manager := newSubscriptionManager()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	ctx := context.Background()

	originalSubscribe := subscribeRemote
	originalUnsubscribe := unsubscribeRemote
	defer func() {
		subscribeRemote = originalSubscribe
		unsubscribeRemote = originalUnsubscribe
	}()

	var handler remote.BranchEventHandler
	subscribeRemote = func(ctx context.Context, remoteURL string, branch string, h remote.BranchEventHandler) error {
		handler = h
		return nil
	}
	unsubscribeRemote = func(ctx context.Context, remoteURL string, branch string) error { return nil }

	var deleted []string
	if err := manager.subscribe(ctx, "git@github.com:org/repo.git", "abc", logger, func(sessionID string) {
		deleted = append(deleted, sessionID)
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if handler == nil {
		t.Fatalf("expected handler to be set")
	}

	events := []remote.BranchEvent{
		{Type: remote.PRClosed, Branch: "abc"},
		{Type: remote.PRMerged, Branch: "def"},
		{Type: remote.BranchDeleted, Branch: "ghi"},
		{Type: remote.BranchCreated, Branch: "skip"},
	}
	for _, event := range events {
		handler(event)
	}

	if len(deleted) != 3 {
		t.Fatalf("expected 3 deletions, got %d", len(deleted))
	}
}
