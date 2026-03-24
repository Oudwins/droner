package remote

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/Oudwins/droner/pkgs/droner/internals/auth"
	"github.com/Oudwins/droner/pkgs/droner/internals/conf"
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

func TestEnsureAuthResolution(t *testing.T) {
	config := conf.GetConfig()
	origDataDir := config.Server.DataDir
	config.Server.DataDir = t.TempDir()
	t.Cleanup(func() { config.Server.DataDir = origDataDir })

	t.Setenv("GITHUB_TOKEN", "env-token")
	if err := auth.WriteGitHubAuth(auth.GitHubAuth{AccessToken: "stored-token", UpdatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("WriteGitHubAuth: %v", err)
	}
	if token, err := resolveGitHubToken(); err != nil || token != "env-token" {
		t.Fatalf("expected env token, got %q err=%v", token, err)
	}

	t.Setenv("GITHUB_TOKEN", "")
	if token, err := resolveGitHubToken(); err != nil || token != "stored-token" {
		t.Fatalf("expected stored token, got %q err=%v", token, err)
	}

	if err := os.RemoveAll(config.Server.DataDir); err != nil {
		t.Fatalf("remove data dir: %v", err)
	}
	ResetRegistryForTests()
	if err := EnsureAuth(context.Background(), "git@github.com:owner/repo.git"); err == nil || err != sdk.ErrAuthRequired {
		t.Fatalf("expected ErrAuthRequired, got %v", err)
	}
}

func TestNewRoundRobinGitHubProviderDefaultsToTenSeconds(t *testing.T) {
	config := conf.GetConfig()
	original := config.Providers.Github.PollInterval
	config.Providers.Github.PollInterval = ""
	t.Cleanup(func() {
		config.Providers.Github.PollInterval = original
	})

	provider := newRoundRobinGitHubProviderWithInterval(newFakeGitHubSDK(), configuredGitHubPollInterval())
	t.Cleanup(provider.close)

	if provider.pollIntervalD != 10*time.Second {
		t.Fatalf("expected 10s default poll interval, got %s", provider.pollIntervalD)
	}
}

func TestConfiguredGitHubPollIntervalHonorsEnvOverride(t *testing.T) {
	config := conf.GetConfig()
	original := config.Providers.Github.PollInterval
	config.Providers.Github.PollInterval = "15s"
	t.Cleanup(func() {
		config.Providers.Github.PollInterval = original
	})
	t.Setenv("REMOTE_POLL_INTERVAL", "2s")

	if got := configuredGitHubPollInterval(); got != 2*time.Second {
		t.Fatalf("expected env override to win, got %s", got)
	}
}
