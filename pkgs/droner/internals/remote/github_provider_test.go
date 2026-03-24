package remote

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeGitHubSDK struct {
	mu            sync.Mutex
	ensureErr     error
	ensureCalls   int
	authenticated bool
	branchData    map[subscriptionKey][]GitHubBranchData
	branchCalls   []subscriptionKey
	branchDataErr error
}

func newFakeGitHubSDK() *fakeGitHubSDK {
	return &fakeGitHubSDK{branchData: make(map[subscriptionKey][]GitHubBranchData)}
}

func (s *fakeGitHubSDK) EnsureAuth() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureCalls++
	return s.ensureErr
}

func (s *fakeGitHubSDK) IsAuthenticated() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.authenticated
}

func (s *fakeGitHubSDK) SetAuthToken(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.authenticated = strings.TrimSpace(token) != ""
}

func (s *fakeGitHubSDK) GetBranchData(ctx context.Context, remoteURL string, branch string) (GitHubBranchData, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := subscriptionKey{remoteURL: remoteURL, branch: branch}
	s.branchCalls = append(s.branchCalls, key)
	if s.branchDataErr != nil {
		return GitHubBranchData{}, s.branchDataErr
	}
	responses := s.branchData[key]
	if len(responses) == 0 {
		return GitHubBranchData{}, nil
	}
	response := responses[0]
	s.branchData[key] = responses[1:]
	return cloneGitHubBranchData(response), nil
}

func TestRoundRobinGitHubProviderPollsSubscriptionsInOrder(t *testing.T) {
	githubSDK := newFakeGitHubSDK()
	handler := func(e BranchEvent) {}
	provider := newGithubProviderDetailed(githubSDK, handler, time.Hour)
	defer provider.close()

	first := subscriptionKey{remoteURL: "git@github.com:org/repo.git", branch: "one"}
	second := subscriptionKey{remoteURL: "git@github.com:org/repo.git", branch: "two"}
	provider.subscribe(first)
	provider.subscribe(second)

	if err := provider.pollNext(context.Background()); err != nil {
		t.Fatalf("first pollNext: %v", err)
	}
	if err := provider.pollNext(context.Background()); err != nil {
		t.Fatalf("second pollNext: %v", err)
	}
	if err := provider.pollNext(context.Background()); err != nil {
		t.Fatalf("third pollNext: %v", err)
	}

	githubSDK.mu.Lock()
	defer githubSDK.mu.Unlock()
	if len(githubSDK.branchCalls) != 3 {
		t.Fatalf("expected 3 SDK calls, got %d", len(githubSDK.branchCalls))
	}
	if githubSDK.branchCalls[0] != first || githubSDK.branchCalls[1] != second || githubSDK.branchCalls[2] != first {
		t.Fatalf("unexpected poll order: %#v", githubSDK.branchCalls)
	}
}

func TestRoundRobinGitHubProviderEmitsTerminalEvents(t *testing.T) {
	githubSDK := newFakeGitHubSDK()
	handler := func(e BranchEvent) {}
	provider := newGithubProviderDetailed(githubSDK, handler, time.Hour)
	defer provider.close()

	key := subscriptionKey{remoteURL: "git@github.com:org/repo.git", branch: "feature"}
	mergedAt := time.Date(2026, time.March, 24, 12, 0, 0, 0, time.UTC)
	githubSDK.branchData[key] = []GitHubBranchData{
		{BranchExists: true, PullRequest: &GitHubPullRequest{Number: 7, State: "open"}},
		{BranchExists: true, PullRequest: &GitHubPullRequest{Number: 7, State: "closed", MergedAt: &mergedAt}},
	}

	received := make(chan BranchEvent, 2)
	provider.eventHandler = func(event BranchEvent) {
		received <- event
	}
	provider.subscribe(key)

	if err := provider.pollNext(context.Background()); err != nil {
		t.Fatalf("first pollNext: %v", err)
	}
	if err := provider.pollNext(context.Background()); err != nil {
		t.Fatalf("second pollNext: %v", err)
	}

	firstEvent := <-received
	secondEvent := <-received
	if firstEvent.Type != PRClosed {
		t.Fatalf("expected first event to be PRClosed, got %s", firstEvent.Type)
	}
	if secondEvent.Type != PRMerged {
		t.Fatalf("expected second event to be PRMerged, got %s", secondEvent.Type)
	}
}

func TestRoundRobinGitHubProviderDelegatesEnsureAuth(t *testing.T) {
	githubSDK := newFakeGitHubSDK()
	handler := func(e BranchEvent) {}
	provider := newGithubProviderDetailed(githubSDK, handler, time.Hour)
	defer provider.close()

	if err := provider.ensureAuth(context.Background(), "git@github.com:org/repo.git"); err != nil {
		t.Fatalf("ensureAuth: %v", err)
	}

	githubSDK.mu.Lock()
	defer githubSDK.mu.Unlock()
	if githubSDK.ensureCalls != 1 {
		t.Fatalf("unexpected ensure auth calls: %#v", githubSDK.ensureCalls)
	}
}
