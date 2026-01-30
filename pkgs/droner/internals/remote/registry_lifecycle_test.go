package remote

import (
	"context"
	"sync"
	"testing"
	"time"
)

type fakeProvider struct {
	mu         sync.Mutex
	events     []BranchEvent
	interval   time.Duration
	ensureErr  error
	ensureCall int
	pollCall   int
}

func (p *fakeProvider) ensureAuth(ctx context.Context, remoteURL string) error {
	p.mu.Lock()
	p.ensureCall++
	p.mu.Unlock()
	return p.ensureErr
}

func (p *fakeProvider) pollInterval() time.Duration {
	return p.interval
}

func (p *fakeProvider) pollEvents(ctx context.Context, remoteURL string, branchName string) ([]BranchEvent, error) {
	p.mu.Lock()
	p.pollCall++
	events := p.events
	p.events = nil
	p.mu.Unlock()
	return events, nil
}

func TestRegistrySubscribeIdempotent(t *testing.T) {
	provider := &fakeProvider{interval: 5 * time.Millisecond}
	reg := &registry{subscriptions: make(map[subscriptionKey]*subscription), provider: provider}

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
}

func TestRegistryUnsubscribeCancels(t *testing.T) {
	provider := &fakeProvider{interval: 5 * time.Millisecond}
	reg := &registry{subscriptions: make(map[subscriptionKey]*subscription), provider: provider}

	ctx := context.Background()
	if err := reg.subscribe(ctx, "git@github.com:org/repo.git", "branch", func(BranchEvent) {}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	key := subscriptionKey{remoteURL: "git@github.com:org/repo.git", branch: "branch"}
	sub := reg.subscriptions[key]
	if sub == nil {
		t.Fatalf("expected subscription")
	}

	if err := reg.unsubscribe(ctx, "git@github.com:org/repo.git", "branch"); err != nil {
		t.Fatalf("unsubscribe: %v", err)
	}

	select {
	case <-sub.ctx.Done():
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("expected subscription context to be cancelled")
	}
	if len(reg.subscriptions) != 0 {
		t.Fatalf("expected no subscriptions")
	}
}

func TestRegistryPollLoopCallsHandler(t *testing.T) {
	events := []BranchEvent{{Type: PRClosed, Branch: "a"}, {Type: PRMerged, Branch: "b"}}
	provider := &fakeProvider{interval: 5 * time.Millisecond, events: events}
	reg := &registry{subscriptions: make(map[subscriptionKey]*subscription), provider: provider}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	received := make(chan BranchEvent, 4)
	if err := reg.subscribe(ctx, "git@github.com:org/repo.git", "branch", func(event BranchEvent) {
		received <- event
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	for i := 0; i < len(events); i++ {
		select {
		case <-received:
		case <-time.After(500 * time.Millisecond):
			t.Fatalf("expected event %d", i)
		}
	}
}
