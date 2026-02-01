package core

import (
	"context"
	"sync"

	"github.com/Oudwins/droner/pkgs/droner/internals/remote"
)

var subscribeRemote = remote.SubscribeBranchEvents
var unsubscribeRemote = remote.UnsubscribeBranchEvents

// subscriptionManager tracks active remote subscriptions for this server
type subscriptionManager struct {
	base *BaseServer
	mu   sync.RWMutex
	subs map[string]struct{} // remoteURL:branch -> subscription
}

func newSubscriptionManager(base *BaseServer) *subscriptionManager {
	return &subscriptionManager{
		subs: make(map[string]struct{}),
		base: base,
	}
}

// subscribe starts a remote subscription if not already active
func (sm *subscriptionManager) subscribe(ctx context.Context, remoteURL string, branch string, onDelete func(sessionID string)) error {
	key := remoteURL + ":" + branch

	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Already subscribed
	if _, exists := sm.subs[key]; exists {
		sm.base.Logger.Info("Remote subscription already active",
			"remote_url", remoteURL,
			"branch", branch,
		)
		return nil
	}

	// Create handler that calls deletion when appropriate events occur
	handler := func(event remote.BranchEvent) {
		sm.base.Logger.Info("Remote event received",
			"event_type", event.Type,
			"remote_url", event.RemoteURL,
			"branch", event.Branch,
			"pr_number", event.PRNumber,
		)

		// Trigger deletion for these event types
		switch event.Type {
		case remote.PRClosed, remote.PRMerged, remote.BranchDeleted:
			sm.base.Logger.Info("Triggering session cleanup due to remote event",
				"event_type", event.Type,
				"branch", event.Branch,
				"pr_number", event.PRNumber,
			)
			// Extract session ID from branch name (branch name == session ID)
			onDelete(event.Branch)
		default:
			sm.base.Logger.Debug("Ignoring non-triggering event",
				"event_type", event.Type,
				"branch", event.Branch,
			)
		}
	}

	// Subscribe via remote package
	if err := subscribeRemote(ctx, remoteURL, branch, handler); err != nil {
		sm.base.Logger.Error("Failed to subscribe to remote events",
			"error", err,
			"remote_url", remoteURL,
			"branch", branch,
		)
		return err
	}

	// Store subscription (using empty struct since remote package manages cancellation)
	sm.subs[key] = struct{}{}

	sm.base.Logger.Info("Remote subscription started",
		"remote_url", remoteURL,
		"branch", branch,
	)

	return nil
}

// unsubscribe stops a remote subscription
func (sm *subscriptionManager) unsubscribe(ctx context.Context, remoteURL string, branch string) error {
	key := remoteURL + ":" + branch

	sm.mu.Lock()
	defer sm.mu.Unlock()

	if _, exists := sm.subs[key]; !exists {
		sm.base.Logger.Info("Remote subscription not found, skipping unsubscribe",
			"remote_url", remoteURL,
			"branch", branch,
		)
		return nil // already unsubscribed
	}

	if err := unsubscribeRemote(ctx, remoteURL, branch); err != nil {
		sm.base.Logger.Error("Failed to unsubscribe from remote events",
			"error", err,
			"remote_url", remoteURL,
			"branch", branch,
		)
		return err
	}

	delete(sm.subs, key)

	sm.base.Logger.Info("Remote subscription stopped",
		"remote_url", remoteURL,
		"branch", branch,
	)

	return nil
}
