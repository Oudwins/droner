package remote

import (
	"context"
	"sync"
	"testing"
)

type fakeProvider struct {
	mu         sync.Mutex
	ensureErr  error
	ensureCall int
	handler    BranchEventHandler
	active     map[subscriptionKey]struct{}
	closed     bool
}

func newFakeProvider() *fakeProvider {
	return &fakeProvider{active: make(map[subscriptionKey]struct{})}
}

func (p *fakeProvider) setEventHandler(handler BranchEventHandler) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.handler = handler
}

func (p *fakeProvider) subscribe(key subscriptionKey) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.active[key] = struct{}{}
}

func (p *fakeProvider) unsubscribe(key subscriptionKey) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.active, key)
}

func (p *fakeProvider) close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
}

func (p *fakeProvider) ensureAuth(ctx context.Context, remoteURL string) error {
	p.mu.Lock()
	p.ensureCall++
	p.mu.Unlock()
	return p.ensureErr
}

func (p *fakeProvider) emit(event BranchEvent) {
	p.mu.Lock()
	handler := p.handler
	p.mu.Unlock()
	if handler != nil {
		handler(event)
	}
}

func (p *fakeProvider) hasSubscription(key subscriptionKey) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, exists := p.active[key]
	return exists
}

func TestRegistrySubscribeIdempotent(t *testing.T) {
	provider := newFakeProvider()
	reg := &registry{subscriptions: make(map[subscriptionKey]*subscription), provider: provider}
	provider.setEventHandler(reg.dispatch)

	ctx := context.Background()
	if err := reg.subscribe(ctx, "git@github.com:org/repo.git", "branch", func(BranchEvent) {}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if err := reg.subscribe(ctx, "git@github.com:org/repo.git", "branch", func(BranchEvent) {}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if len(reg.subscriptions) != 1 {
		t.Fatalf("expected one subscription, got %d", len(reg.subscriptions))
	}
	key := subscriptionKey{remoteURL: "git@github.com:org/repo.git", branch: "branch"}
	if !provider.hasSubscription(key) {
		t.Fatalf("expected provider subscription")
	}
	if provider.ensureCall != 2 {
		t.Fatalf("expected ensure auth to be called twice, got %d", provider.ensureCall)
	}
}

func TestRegistryUnsubscribeRemovesProviderSubscription(t *testing.T) {
	provider := newFakeProvider()
	reg := &registry{subscriptions: make(map[subscriptionKey]*subscription), provider: provider}
	provider.setEventHandler(reg.dispatch)

	ctx := context.Background()
	if err := reg.subscribe(ctx, "git@github.com:org/repo.git", "branch", func(BranchEvent) {}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	key := subscriptionKey{remoteURL: "git@github.com:org/repo.git", branch: "branch"}
	if err := reg.unsubscribe(ctx, key.remoteURL, key.branch); err != nil {
		t.Fatalf("unsubscribe: %v", err)
	}

	if len(reg.subscriptions) != 0 {
		t.Fatalf("expected no subscriptions")
	}
	if provider.hasSubscription(key) {
		t.Fatalf("expected provider subscription to be removed")
	}
}

func TestRegistryDispatchCallsMatchingHandler(t *testing.T) {
	provider := newFakeProvider()
	reg := &registry{subscriptions: make(map[subscriptionKey]*subscription), provider: provider}
	provider.setEventHandler(reg.dispatch)

	received := make(chan BranchEvent, 1)
	ctx := context.Background()
	if err := reg.subscribe(ctx, "git@github.com:org/repo.git", "branch", func(event BranchEvent) {
		received <- event
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	provider.emit(BranchEvent{Type: PRMerged, RemoteURL: "git@github.com:org/repo.git", Branch: "branch"})

	select {
	case event := <-received:
		if event.Type != PRMerged {
			t.Fatalf("expected merged event, got %s", event.Type)
		}
	default:
		t.Fatalf("expected event")
	}
}
