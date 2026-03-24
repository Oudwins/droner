package remote

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Oudwins/droner/pkgs/droner/sdk"
)

func TestParseGitHubURL(t *testing.T) {
	owner, repo, err := parseGitHubURL("git@github.com:owner/repo.git")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if owner != "owner" || repo != "repo" {
		t.Fatalf("unexpected parse result: %s/%s", owner, repo)
	}

	owner, repo, err = parseGitHubURL("https://github.com/owner/repo.git")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if owner != "owner" || repo != "repo" {
		t.Fatalf("unexpected parse result: %s/%s", owner, repo)
	}

	if _, _, err := parseGitHubURL("git@github.com:owner"); err == nil {
		t.Fatalf("expected error for invalid ssh url")
	}
	if _, _, err := parseGitHubURL("https://example.com/owner/repo.git"); err == nil {
		t.Fatalf("expected error for non-github url")
	}
}

func TestLiveGitHubSDKGetBranchData(t *testing.T) {
	mergedAt := time.Date(2026, time.March, 24, 12, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/owner/repo/git/ref/heads/feature":
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == "/repos/owner/repo/pulls":
			query := r.URL.Query()
			if query.Get("head") != "owner:feature" {
				t.Fatalf("unexpected head query: %q", query.Get("head"))
			}
			if query.Get("state") != "all" {
				t.Fatalf("unexpected state query: %q", query.Get("state"))
			}
			if query.Get("per_page") != "1" {
				t.Fatalf("unexpected per_page query: %q", query.Get("per_page"))
			}
			_, _ = w.Write([]byte(`[{"number":42,"state":"closed","merged_at":"2026-03-24T12:00:00Z","title":"Ship it","html_url":"https://github.com/owner/repo/pull/42","head":{"ref":"feature"},"base":{"ref":"main"}}]`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	githubSDK := newLiveGitHubSDK()
	githubSDK.apiBaseURL = server.URL
	githubSDK.SetAuthToken("test-token")

	data, err := githubSDK.GetBranchData(context.Background(), "git@github.com:owner/repo.git", "feature")
	if err != nil {
		t.Fatalf("GetBranchData: %v", err)
	}
	if !data.BranchExists {
		t.Fatalf("expected branch to exist")
	}
	if data.PullRequest == nil {
		t.Fatalf("expected pull request")
	}
	if data.PullRequest.Number != 42 {
		t.Fatalf("expected PR number 42, got %d", data.PullRequest.Number)
	}
	if data.PullRequest.Head.Ref != "feature" || data.PullRequest.Base.Ref != "main" {
		t.Fatalf("unexpected ref names: head=%q base=%q", data.PullRequest.Head.Ref, data.PullRequest.Base.Ref)
	}
	if data.PullRequest.HTMLURL != "https://github.com/owner/repo/pull/42" {
		t.Fatalf("unexpected PR URL: %q", data.PullRequest.HTMLURL)
	}
	if data.PullRequest.MergedAt == nil || !data.PullRequest.MergedAt.Equal(mergedAt) {
		t.Fatalf("unexpected merged time: %v", data.PullRequest.MergedAt)
	}
}

func TestLiveGitHubSDKAuthState(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "env-token")
	githubSDK := newLiveGitHubSDK()

	if !githubSDK.IsAuthenticated() {
		t.Fatalf("expected SDK to use env token")
	}
	if err := githubSDK.EnsureAuth(); err != nil {
		t.Fatalf("EnsureAuth with env token: %v", err)
	}

	githubSDK.SetAuthToken("")
	if githubSDK.IsAuthenticated() {
		t.Fatalf("expected SDK to be unauthenticated after clearing token")
	}
	if err := githubSDK.EnsureAuth(); err != sdk.ErrAuthRequired {
		t.Fatalf("expected ErrAuthRequired after clearing token, got %v", err)
	}

	githubSDK.SetAuthToken("manual-token")
	if err := githubSDK.EnsureAuth(); err != nil {
		t.Fatalf("EnsureAuth with manual token: %v", err)
	}
}

func TestEnsureAuthUsesCurrentEnvToken(t *testing.T) {
	t.Cleanup(ResetRegistryForTests)

	t.Setenv("GITHUB_TOKEN", "env-token")
	ResetRegistryForTests()
	if err := EnsureAuth(context.Background(), "git@github.com:owner/repo.git"); err != nil {
		t.Fatalf("EnsureAuth with env token: %v", err)
	}

	t.Setenv("GITHUB_TOKEN", "")
	ResetRegistryForTests()
	if err := EnsureAuth(context.Background(), "git@github.com:owner/repo.git"); err != sdk.ErrAuthRequired {
		t.Fatalf("expected ErrAuthRequired without token, got %v", err)
	}
}
