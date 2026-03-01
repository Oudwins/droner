package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Oudwins/droner/pkgs/droner/internals/auth"
	"github.com/Oudwins/droner/pkgs/droner/internals/conf"
	"github.com/Oudwins/droner/pkgs/droner/internals/timeouts"
	"github.com/Oudwins/droner/pkgs/droner/sdk"
)

// githubProvider implements the provider interface for GitHub
type githubProvider struct {
	token         string
	pollIntervalD time.Duration
	apiBaseURL    string
	httpClient    *http.Client

	mu    sync.Mutex
	state map[string]githubBranchState
}

type githubBranchState struct {
	initialized  bool
	branchExists bool
	hasPR        bool
	prNumber     int
	prState      string
	merged       bool
}

type githubPull struct {
	Number   int        `json:"number"`
	State    string     `json:"state"`
	MergedAt *time.Time `json:"merged_at"`
}

func newGitHubProvider() *githubProvider {
	interval := timeouts.PollInterval
	if configured := strings.TrimSpace(conf.GetConfig().Providers.Github.PollInterval); configured != "" {
		if d, err := time.ParseDuration(configured); err == nil {
			interval = d
		}
	}
	if val := os.Getenv("REMOTE_POLL_INTERVAL"); val != "" {
		if d, err := time.ParseDuration(val); err == nil {
			interval = d
		}
	}

	return &githubProvider{
		token:         os.Getenv("GITHUB_TOKEN"),
		pollIntervalD: interval,
		apiBaseURL:    "https://api.github.com",
		httpClient:    &http.Client{Timeout: timeouts.SecondDefault},
		state:         make(map[string]githubBranchState),
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
	if !isGitHubURL(remoteURL) {
		return []BranchEvent{}, nil
	}

	owner, repo, err := p.parseGitHubURL(remoteURL)
	if err != nil {
		return nil, err
	}

	branchExists, err := p.fetchBranchExists(ctx, owner, repo, branchName)
	if err != nil {
		return nil, err
	}

	pull, hasPR, err := p.fetchPullForBranch(ctx, owner, repo, branchName)
	if err != nil {
		return nil, err
	}

	key := remoteURL + "\n" + branchName
	current := githubBranchState{initialized: true, branchExists: branchExists}
	if hasPR {
		current.hasPR = true
		current.prNumber = pull.Number
		current.prState = pull.State
		current.merged = pull.MergedAt != nil
	}

	p.mu.Lock()
	prev := p.state[key]
	p.state[key] = current
	p.mu.Unlock()

	if !prev.initialized {
		return []BranchEvent{}, nil
	}

	events := make([]BranchEvent, 0, 2)
	now := time.Now()
	if prev.branchExists && !current.branchExists {
		events = append(events, BranchEvent{Type: BranchDeleted, RemoteURL: remoteURL, Branch: branchName, Timestamp: now})
	}
	if prev.hasPR && current.hasPR {
		if prev.prState == "open" && current.prState == "closed" {
			prState := current.prState
			n := current.prNumber
			events = append(events, BranchEvent{Type: PRClosed, RemoteURL: remoteURL, Branch: branchName, PRNumber: &n, PRState: &prState, Timestamp: now})
		}
		if !prev.merged && current.merged {
			prState := current.prState
			n := current.prNumber
			events = append(events, BranchEvent{Type: PRMerged, RemoteURL: remoteURL, Branch: branchName, PRNumber: &n, PRState: &prState, Timestamp: now})
		}
	}

	return events, nil
}

func (p *githubProvider) fetchBranchExists(ctx context.Context, owner string, repo string, branchName string) (bool, error) {
	ref := "heads/" + branchName
	escapedRef := url.PathEscape(ref)
	u := fmt.Sprintf("%s/repos/%s/%s/git/ref/%s", p.apiBaseURL, owner, repo, escapedRef)
	status, _, err := p.doGET(ctx, u)
	if err != nil {
		return false, err
	}
	switch status {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	default:
		return false, fmt.Errorf("unexpected github status checking branch ref: %d", status)
	}
}

func (p *githubProvider) fetchPullForBranch(ctx context.Context, owner string, repo string, branchName string) (githubPull, bool, error) {
	q := url.Values{}
	q.Set("head", owner+":"+branchName)
	q.Set("state", "all")
	q.Set("per_page", "1")
	u := fmt.Sprintf("%s/repos/%s/%s/pulls?%s", p.apiBaseURL, owner, repo, q.Encode())
	status, body, err := p.doGET(ctx, u)
	if err != nil {
		return githubPull{}, false, err
	}
	if status != http.StatusOK {
		return githubPull{}, false, fmt.Errorf("unexpected github status listing pulls: %d", status)
	}
	var pulls []githubPull
	if err := json.Unmarshal(body, &pulls); err != nil {
		return githubPull{}, false, fmt.Errorf("failed to parse github pulls response: %w", err)
	}
	if len(pulls) == 0 {
		return githubPull{}, false, nil
	}
	return pulls[0], true, nil
}

func (p *githubProvider) doGET(ctx context.Context, requestURL string) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "droner")
	if strings.TrimSpace(p.token) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(p.token))
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, err
	}
	return resp.StatusCode, body, nil
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
