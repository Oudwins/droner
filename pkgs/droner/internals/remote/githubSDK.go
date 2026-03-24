package remote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/Oudwins/droner/pkgs/droner/internals/timeouts"
	"github.com/Oudwins/droner/pkgs/droner/sdk"
)

type GitHubBranchData struct {
	BranchExists bool
	PullRequest  *GitHubPullRequest
}

type GitHubPullRequest struct {
	Number   int             `json:"number"`
	State    string          `json:"state"`
	MergedAt *time.Time      `json:"merged_at,omitempty"`
	Title    string          `json:"title"`
	HTMLURL  string          `json:"html_url"`
	Head     GitHubBranchRef `json:"head"`
	Base     GitHubBranchRef `json:"base"`
}

type GitHubBranchRef struct {
	Ref string `json:"ref"`
}

type GitHubSDK interface {
	IsAuthenticated() bool
	EnsureAuth() error
	GetBranchData(ctx context.Context, remoteURL string, branch string) (GitHubBranchData, error)
	SetAuthToken(token string)
}

type liveGitHubSDK struct {
	token      string
	apiBaseURL string
	httpClient *http.Client
}

func newLiveGitHubSDK() *liveGitHubSDK {
	return &liveGitHubSDK{
		token:      os.Getenv("GITHUB_TOKEN"),
		apiBaseURL: "https://api.github.com",
		httpClient: &http.Client{Timeout: timeouts.SecondDefault},
	}
}

func (s *liveGitHubSDK) SetAuthToken(token string) {
	s.token = strings.TrimSpace(token)
}

func (s *liveGitHubSDK) IsAuthenticated() bool {
	return s.token != ""
}

func (s *liveGitHubSDK) EnsureAuth() error {
	if !s.IsAuthenticated() {
		return sdk.ErrAuthRequired
	}
	return nil
}

func (s *liveGitHubSDK) GetBranchData(ctx context.Context, remoteURL string, branch string) (GitHubBranchData, error) {
	if !isGitHubURL(remoteURL) {
		return GitHubBranchData{}, errors.New("Remote URL is not a github URL")
	}
	if !s.IsAuthenticated() {
		return GitHubBranchData{}, sdk.ErrAuthRequired
	}

	owner, repo, err := parseGitHubURL(remoteURL)
	if err != nil {
		return GitHubBranchData{}, err
	}

	branchExists, err := s.fetchBranchExists(ctx, owner, repo, branch)
	if err != nil {
		return GitHubBranchData{}, err
	}

	pullRequest, err := s.fetchPullRequestForBranch(ctx, owner, repo, branch)
	if err != nil {
		return GitHubBranchData{}, err
	}

	return GitHubBranchData{BranchExists: branchExists, PullRequest: pullRequest}, nil
}

func (s *liveGitHubSDK) fetchBranchExists(ctx context.Context, owner string, repo string, branch string) (bool, error) {
	ref := "heads/" + branch
	escapedRef := url.PathEscape(ref)
	requestURL := fmt.Sprintf("%s/repos/%s/%s/git/ref/%s", s.apiBaseURL, owner, repo, escapedRef)
	status, _, err := s.doGET(ctx, requestURL)
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

func (s *liveGitHubSDK) fetchPullRequestForBranch(ctx context.Context, owner string, repo string, branch string) (*GitHubPullRequest, error) {
	q := url.Values{}
	q.Set("head", owner+":"+branch)
	q.Set("state", "all")
	q.Set("per_page", "1")

	requestURL := fmt.Sprintf("%s/repos/%s/%s/pulls?%s", s.apiBaseURL, owner, repo, q.Encode())
	status, body, err := s.doGET(ctx, requestURL)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("unexpected github status listing pulls: %d", status)
	}

	var pulls []GitHubPullRequest
	if err := json.Unmarshal(body, &pulls); err != nil {
		return nil, fmt.Errorf("failed to parse github pulls response: %w", err)
	}
	if len(pulls) == 0 {
		return nil, nil
	}

	pull := pulls[0]
	return &pull, nil
}

func (s *liveGitHubSDK) doGET(ctx context.Context, requestURL string) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "droner")
	if strings.TrimSpace(s.token) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(s.token))
	}

	resp, err := s.httpClient.Do(req)
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

func parseGitHubURL(remoteURL string) (string, string, error) {
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
