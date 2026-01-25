package remote

import (
	"context"
	"sync"
	"time"
)

// BranchEventType represents the type of branch/PR event
type BranchEventType string

const (
	PRCreated     BranchEventType = "pr_created"
	PRClosed      BranchEventType = "pr_closed"
	PRMerged      BranchEventType = "pr_merged"
	PRDeleted     BranchEventType = "pr_deleted"
	BranchCreated BranchEventType = "branch_created"
	BranchDeleted BranchEventType = "branch_deleted"
)

// BranchEvent represents a branch or PR lifecycle event
type BranchEvent struct {
	Type      BranchEventType `json:"type"`
	RemoteURL string          `json:"remote_url"`
	Branch    string          `json:"branch"`
	PRNumber  *int            `json:"pr_number,omitempty"`
	PRState   *string         `json:"pr_state,omitempty"`
	Timestamp time.Time       `json:"timestamp"`
}

// BranchEventHandler is a function that handles branch events
type BranchEventHandler func(event BranchEvent)

// subscriptionKey uniquely identifies a subscription
type subscriptionKey struct {
	remoteURL string
	branch    string
}

// subscription represents an active subscription
type subscription struct {
	ctx     context.Context
	cancel  context.CancelFunc
	handler BranchEventHandler
}

// registry manages active subscriptions
type registry struct {
	mu            sync.RWMutex
	subscriptions map[subscriptionKey]*subscription
	provider      provider
}

var globalRegistry *registry
var once sync.Once

func getRegistry() *registry {
	once.Do(func() {
		globalRegistry = &registry{
			subscriptions: make(map[subscriptionKey]*subscription),
			provider:      newGitHubProvider(),
		}
	})
	return globalRegistry
}

func (r *registry) subscribe(ctx context.Context, remoteURL string, branchName string, handler BranchEventHandler) error {
	key := subscriptionKey{remoteURL: remoteURL, branch: branchName}

	r.mu.Lock()
	defer r.mu.Unlock()

	// If already subscribed, just update handler
	if sub, exists := r.subscriptions[key]; exists {
		sub.handler = handler
		return nil
	}

	// Create new subscription
	subCtx, cancel := context.WithCancel(ctx)
	sub := &subscription{
		ctx:     subCtx,
		cancel:  cancel,
		handler: handler,
	}

	r.subscriptions[key] = sub

	// Start polling in background
	go r.pollLoop(subCtx, remoteURL, branchName, handler)

	return nil
}

func (r *registry) unsubscribe(ctx context.Context, remoteURL string, branchName string) error {
	key := subscriptionKey{remoteURL: remoteURL, branch: branchName}

	r.mu.Lock()
	defer r.mu.Unlock()

	if sub, exists := r.subscriptions[key]; exists {
		sub.cancel()
		delete(r.subscriptions, key)
	}

	return nil
}

func (r *registry) pollLoop(ctx context.Context, remoteURL string, branchName string, handler BranchEventHandler) {
	ticker := time.NewTicker(r.provider.pollInterval())
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			events, err := r.provider.pollEvents(ctx, remoteURL, branchName)
			if err != nil {
				// Log error but continue polling
				continue
			}
			for _, event := range events {
				handler(event)
			}
		}
	}
}

// SubscribeBranchEvents subscribes to branch/PR events for a given remote URL and branch
// Returns an error if subscription fails. Subscribe is idempotent.
func SubscribeBranchEvents(ctx context.Context, remoteURL string, branchName string, handler BranchEventHandler) error {
	return getRegistry().subscribe(ctx, remoteURL, branchName, handler)
}

// UnsubscribeBranchEvents unsubscribes from branch/PR events for a given remote URL and branch
// Returns an error if unsubscription fails. Unsubscribe is idempotent.
func UnsubscribeBranchEvents(ctx context.Context, remoteURL string, branchName string) error {
	return getRegistry().unsubscribe(ctx, remoteURL, branchName)
}
