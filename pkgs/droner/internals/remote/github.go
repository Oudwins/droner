package remote

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/Oudwins/droner/pkgs/droner/internals/auth"
	"github.com/Oudwins/droner/pkgs/droner/sdk"
)

// githubProvider implements the provider interface for GitHub
type githubProvider struct {
	token         string
	pollIntervalD time.Duration
}

func newGitHubProvider() *githubProvider {
	interval := 1 * time.Hour // default
	if val := os.Getenv("REMOTE_POLL_INTERVAL"); val != "" {
		if d, err := time.ParseDuration(val); err == nil {
			interval = d
		}
	}

	return &githubProvider{
		token:         os.Getenv("GITHUB_TOKEN"),
		pollIntervalD: interval,
	}
}

func (p *githubProvider) ensureAuth(ctx context.Context, remoteURL string) error {
	if !isGitHubURL(remoteURL) {
		return nil
	}

	token, err := resolveGitHubToken()
	if err != nil {
		return err
	}
	if token == "" {
		return sdk.ErrAuthRequired
	}
	p.token = token
	return nil
}

func (p *githubProvider) pollInterval() time.Duration {
	return p.pollIntervalD
}

func (p *githubProvider) pollEvents(ctx context.Context, remoteURL string, branchName string) ([]BranchEvent, error) {
	owner, repo, err := p.parseGitHubURL(remoteURL)
	if err != nil {
		return nil, err
	}

	// TODO: Implement actual GitHub API calls
	// For now, return empty slice - will implement actual polling in next step
	// This is just to make the interface compile
	_ = owner
	_ = repo
	_ = branchName
	return []BranchEvent{}, nil
}

func (p *githubProvider) parseGitHubURL(remoteURL string) (string, string, error) {
	// Handle SSH URLs: git@github.com:owner/repo.git
	if strings.HasPrefix(remoteURL, "git@") {
		parts := strings.Split(remoteURL, ":")
		if len(parts) != 2 {
			return "", "", fmt.Errorf("invalid SSH GitHub URL: %s", remoteURL)
		}
		repoPart := strings.TrimSuffix(parts[1], ".git")
		repoParts := strings.Split(repoPart, "/")
		if len(repoParts) != 2 {
			return "", "", fmt.Errorf("invalid SSH GitHub URL format: %s", remoteURL)
		}
		return repoParts[0], repoParts[1], nil
	}

	// Handle HTTPS URLs: https://github.com/owner/repo.git
	parsed, err := url.Parse(remoteURL)
	if err != nil {
		return "", "", fmt.Errorf("invalid GitHub URL: %s", remoteURL)
	}

	if parsed.Host != "github.com" {
		return "", "", fmt.Errorf("not a GitHub URL: %s", remoteURL)
	}

	path := strings.TrimPrefix(parsed.Path, "/")
	path = strings.TrimSuffix(path, ".git")
	parts := strings.Split(path, "/")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid GitHub URL format: %s", remoteURL)
	}

	return parts[0], parts[1], nil
}

func resolveGitHubToken() (string, error) {
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		return token, nil
	}

	stored, ok, err := auth.ReadGitHubAuth()
	if err != nil {
		return "", err
	}
	if !ok {
		return "", nil
	}

	return strings.TrimSpace(stored.AccessToken), nil
}

func isGitHubURL(remoteURL string) bool {
	if strings.HasPrefix(remoteURL, "git@github.com:") {
		return true
	}
	parsed, err := url.Parse(remoteURL)
	if err != nil {
		return false
	}
	return parsed.Host == "github.com"
}
