package remote

import (
	"context"
	"sync"
	"time"

	"github.com/Oudwins/droner/pkgs/droner/internals/conf"
)

type roundRobinGitHubProvider struct {
	githubSDK     GitHubSDK
	pollIntervalD time.Duration
	eventHandler  BranchEventHandler
	stop          context.CancelFunc

	mu            sync.Mutex
	state         map[subscriptionKey]githubBranchState
	subscriptions map[subscriptionKey]struct{}
	order         []subscriptionKey
	nextIndex     int
}

type githubBranchState struct {
	initialized bool
	data        GitHubBranchData
}

func newGithubProvider(handler BranchEventHandler) provider {
	return newGithubProviderDetailed(newLiveGitHubSDK(), handler, time.Duration(conf.GetConfig().Providers.Github.PollInterval))
}

func newGithubProviderDetailed(gh GitHubSDK, handler BranchEventHandler, interval time.Duration) *roundRobinGitHubProvider {
	ctx, cancel := context.WithCancel(context.Background())
	p := &roundRobinGitHubProvider{
		githubSDK:     gh,
		pollIntervalD: interval,
		state:         make(map[subscriptionKey]githubBranchState),
		subscriptions: make(map[subscriptionKey]struct{}),
		stop:          cancel,
		eventHandler:  handler,
	}
	go p.run(ctx)
	return p

}

func (p *roundRobinGitHubProvider) ensureAuth(ctx context.Context, remoteURL string) error {
	return p.githubSDK.EnsureAuth()
}

func (p *roundRobinGitHubProvider) subscribe(key subscriptionKey) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, exists := p.subscriptions[key]; exists {
		return
	}
	p.subscriptions[key] = struct{}{}
	p.order = append(p.order, key)
}

func (p *roundRobinGitHubProvider) unsubscribe(key subscriptionKey) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, exists := p.subscriptions[key]; !exists {
		return
	}

	delete(p.subscriptions, key)
	delete(p.state, key)

	// TODO this is nasty maybe we can do something different
	for i, candidate := range p.order {
		if candidate != key {
			continue
		}
		p.order = append(p.order[:i], p.order[i+1:]...)
		if i < p.nextIndex {
			p.nextIndex--
		}
		if p.nextIndex >= len(p.order) {
			p.nextIndex = 0
		}
		break
	}
}

func (p *roundRobinGitHubProvider) close() {
	if p.stop != nil {
		p.stop()
	}
}

func (p *roundRobinGitHubProvider) run(ctx context.Context) {
	ticker := time.NewTicker(p.pollIntervalD)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = p.pollNext(ctx)
		}
	}
}

func (p *roundRobinGitHubProvider) pollNext(ctx context.Context) error {
	if !p.githubSDK.IsAuthenticated() {
		return nil
	}

	key, ok := p.nextSubscription()
	if !ok {
		return nil
	}
	return p.pollSubscription(ctx, key)
}

func (p *roundRobinGitHubProvider) nextSubscription() (subscriptionKey, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.order) == 0 {
		return subscriptionKey{}, false
	}
	if p.nextIndex >= len(p.order) {
		p.nextIndex = 0
	}
	key := p.order[p.nextIndex]
	p.nextIndex = (p.nextIndex + 1) % len(p.order)
	return key, true
}

func (p *roundRobinGitHubProvider) pollSubscription(ctx context.Context, key subscriptionKey) error {
	if !isGitHubURL(key.remoteURL) {
		return nil
	}

	branchData, err := p.githubSDK.GetBranchData(ctx, key.remoteURL, key.branch)
	if err != nil {
		return err
	}

	events := p.storeBranchData(key, branchData)
	handler := p.currentEventHandler()
	if handler == nil {
		return nil
	}

	for _, event := range events {
		handler(event)
	}
	return nil
}

func (p *roundRobinGitHubProvider) storeBranchData(key subscriptionKey, branchData GitHubBranchData) []BranchEvent {
	currentData := branchData
	current := githubBranchState{initialized: true, data: currentData}

	p.mu.Lock()
	if _, exists := p.subscriptions[key]; !exists {
		p.mu.Unlock()
		return nil
	}
	previous := p.state[key]
	p.state[key] = current
	p.mu.Unlock()

	if !previous.initialized {
		return nil
	}

	return diffGitHubBranchState(key, previous.data, currentData)
}

func (p *roundRobinGitHubProvider) currentEventHandler() BranchEventHandler {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.eventHandler
}

func diffGitHubBranchState(key subscriptionKey, previous GitHubBranchData, current GitHubBranchData) []BranchEvent {
	events := make([]BranchEvent, 0, 2)
	now := time.Now()

	if previous.BranchExists && !current.BranchExists {
		events = append(events, BranchEvent{Type: BranchDeleted, RemoteURL: key.remoteURL, Branch: key.branch, Timestamp: now})
	}

	previousPR := previous.PullRequest
	currentPR := current.PullRequest
	if previousPR != nil && currentPR != nil {
		if previousPR.State == "open" && currentPR.State == "closed" {
			prState := currentPR.State
			number := currentPR.Number
			events = append(events, BranchEvent{Type: PRClosed, RemoteURL: key.remoteURL, Branch: key.branch, PRNumber: &number, PRState: &prState, Timestamp: now})
		}
		if previousPR.MergedAt == nil && currentPR.MergedAt != nil {
			prState := currentPR.State
			number := currentPR.Number
			events = append(events, BranchEvent{Type: PRMerged, RemoteURL: key.remoteURL, Branch: key.branch, PRNumber: &number, PRState: &prState, Timestamp: now})
		}
	}

	return events
}
