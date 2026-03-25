package remote

import (
	"context"
	"errors"
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

var ErrAuthRequired = errors.New("github auth required")

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
		}
		provider := newGithubProvider(globalRegistry.dispatch)
		globalRegistry.provider = provider
	})
	return globalRegistry
}

func (r *registry) subscribe(ctx context.Context, remoteURL string, branchName string, handler BranchEventHandler) error {
	key := subscriptionKey{remoteURL: remoteURL, branch: branchName}

	if err := r.provider.ensureAuth(); err != nil {
		return err
	}

	r.mu.Lock()

	// If already subscribed, just update handler
	if sub, exists := r.subscriptions[key]; exists {
		sub.handler = handler
		r.mu.Unlock()
		return nil
	}

	r.subscriptions[key] = &subscription{handler: handler}
	r.mu.Unlock()
	r.provider.subscribe(key)

	return nil
}

func (r *registry) unsubscribe(ctx context.Context, remoteURL string, branchName string) error {
	key := subscriptionKey{remoteURL: remoteURL, branch: branchName}

	r.mu.Lock()
	_, exists := r.subscriptions[key]
	if exists {
		delete(r.subscriptions, key)
	}
	r.mu.Unlock()

	if exists {
		r.provider.unsubscribe(key)
	}

	return nil
}

func (r *registry) dispatch(event BranchEvent) {
	key := subscriptionKey{remoteURL: event.RemoteURL, branch: event.Branch}

	r.mu.RLock()
	sub := r.subscriptions[key]
	if sub == nil {
		r.mu.RUnlock()
		return
	}
	h := sub.handler
	r.mu.RUnlock()

	h(event)
}

// EnsureAuth validates provider auth for the remote URL.
func EnsureAuth(ctx context.Context, remoteURL string) error {
	return getRegistry().provider.ensureAuth()
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
