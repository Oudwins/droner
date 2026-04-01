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
	PRCreated     BranchEventType = "pr.created"
	PRClosed      BranchEventType = "pr.closed"
	PRMerged      BranchEventType = "pr.merged"
	PRDeleted     BranchEventType = "pr.deleted"
	BranchCreated BranchEventType = "branch.created"
	BranchDeleted BranchEventType = "branch.deleted"
)

var ErrUnsupportedRemote = errors.New("unsupported remote provider")

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
	providers     []remoteProvider
}

var globalRegistry *registry
var once sync.Once

func getRegistry() *registry {
	once.Do(func() {
		globalRegistry = &registry{
			subscriptions: make(map[subscriptionKey]*subscription),
			providers:     []remoteProvider{},
		}
		globalRegistry.providers = append(globalRegistry.providers, newGithubProvider(globalRegistry.dispatch))
	})
	return globalRegistry
}

func (r *registry) subscribe(ctx context.Context, remoteURL string, branchName string, handler BranchEventHandler) error {
	_ = ctx
	key := subscriptionKey{remoteURL: remoteURL, branch: branchName}
	logger := remoteLogger()
	logger.Debug("subscribe requested",
		"remote_url", remoteURL,
		"branch", branchName,
	)

	if p, ok := r.providerForKey(key); ok {
		if err := p.ensureAuth(); err != nil {
			logger.Debug("subscribe failed auth",
				"remote_url", remoteURL,
				"branch", branchName,
				"error", err,
			)
			return err
		}

		r.mu.Lock()
		r.subscriptions[key] = &subscription{handler: handler}
		r.mu.Unlock()
		logger.Debug("subscription stored",
			"remote_url", remoteURL,
			"branch", branchName,
		)
		p.subscribe(key)
		logger.Debug("provider subscribed",
			"remote_url", remoteURL,
			"branch", branchName,
		)

		return nil
	}

	logger.Debug("subscribe unsupported",
		"remote_url", remoteURL,
		"branch", branchName,
	)
	return ErrUnsupportedRemote
}

func (r *registry) unsubscribe(ctx context.Context, remoteURL string, branchName string) error {
	_ = ctx
	key := subscriptionKey{remoteURL: remoteURL, branch: branchName}
	logger := remoteLogger()
	logger.Debug("unsubscribe requested",
		"remote_url", remoteURL,
		"branch", branchName,
	)

	r.mu.Lock()
	delete(r.subscriptions, key)
	r.mu.Unlock()
	logger.Debug("subscription removed",
		"remote_url", remoteURL,
		"branch", branchName,
	)

	if p, ok := r.providerForKey(key); ok {
		p.unsubscribe(key)
		logger.Debug("provider unsubscribed",
			"remote_url", remoteURL,
			"branch", branchName,
		)
		return nil
	}
	logger.Debug("unsubscribe unsupported",
		"remote_url", remoteURL,
		"branch", branchName,
	)
	return ErrUnsupportedRemote
}

func (r *registry) dispatch(event BranchEvent) {
	key := subscriptionKey{remoteURL: event.RemoteURL, branch: event.Branch}
	logger := remoteLogger()

	r.mu.RLock()
	sub := r.subscriptions[key]
	if sub == nil {
		r.mu.RUnlock()
		logger.Debug("event dropped without subscription",
			"event_type", event.Type,
			"remote_url", event.RemoteURL,
			"branch", event.Branch,
			"pr_number", event.PRNumber,
		)
		return
	}
	h := sub.handler
	r.mu.RUnlock()

	logger.Debug("event dispatched",
		"event_type", event.Type,
		"remote_url", event.RemoteURL,
		"branch", event.Branch,
		"pr_number", event.PRNumber,
	)

	h(event)
}

func (r *registry) providerForKey(key subscriptionKey) (remoteProvider, bool) {
	for _, p := range r.providers {
		if p.isValidKey(key) {
			return p, true
		}
	}
	return nil, false
}

func (r *registry) close() {
	for _, p := range r.providers {
		p.close()
	}
}

// EnsureAuth validates provider auth for the remote URL.
func EnsureAuth(ctx context.Context, remoteURL string) error {
	_ = ctx
	key := subscriptionKey{remoteURL: remoteURL}
	if p, ok := getRegistry().providerForKey(key); ok {
		return p.ensureAuth()
	}
	return ErrUnsupportedRemote
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
