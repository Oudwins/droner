package remote

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/Oudwins/droner/pkgs/droner/internals/auth"
	"github.com/Oudwins/droner/pkgs/droner/internals/conf"
	"github.com/Oudwins/droner/pkgs/droner/sdk"
)

func TestParseGitHubURL(t *testing.T) {
	provider := newGitHubProvider()

	owner, repo, err := provider.parseGitHubURL("git@github.com:owner/repo.git")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if owner != "owner" || repo != "repo" {
		t.Fatalf("unexpected parse result: %s/%s", owner, repo)
	}

	owner, repo, err = provider.parseGitHubURL("https://github.com/owner/repo.git")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if owner != "owner" || repo != "repo" {
		t.Fatalf("unexpected parse result: %s/%s", owner, repo)
	}

	if _, _, err := provider.parseGitHubURL("git@github.com:owner"); err == nil {
		t.Fatalf("expected error for invalid ssh url")
	}
	if _, _, err := provider.parseGitHubURL("https://example.com/owner/repo.git"); err == nil {
		t.Fatalf("expected error for non-github url")
	}
}

func TestEnsureAuthResolution(t *testing.T) {
	config := conf.GetConfig()
	origDataDir := config.Server.DataDir
	config.Server.DataDir = t.TempDir()
	t.Cleanup(func() { config.Server.DataDir = origDataDir })

	// env token wins
	t.Setenv("GITHUB_TOKEN", "env-token")
	if err := auth.WriteGitHubAuth(auth.GitHubAuth{AccessToken: "stored-token", UpdatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("WriteGitHubAuth: %v", err)
	}
	if token, err := resolveGitHubToken(); err != nil || token != "env-token" {
		t.Fatalf("expected env token, got %q err=%v", token, err)
	}

	// stored token used when env missing
	t.Setenv("GITHUB_TOKEN", "")
	if token, err := resolveGitHubToken(); err != nil || token != "stored-token" {
		t.Fatalf("expected stored token, got %q err=%v", token, err)
	}

	// no token returns auth required
	if err := os.RemoveAll(config.Server.DataDir); err != nil {
		t.Fatalf("remove data dir: %v", err)
	}
	ResetRegistryForTests()
	if err := EnsureAuth(context.Background(), "git@github.com:owner/repo.git"); err == nil || err != sdk.ErrAuthRequired {
		t.Fatalf("expected ErrAuthRequired, got %v", err)
	}
}
