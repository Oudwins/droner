package remote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Oudwins/droner/pkgs/droner/dronerd/internals/auth"
	"github.com/Oudwins/droner/pkgs/droner/internals/timeouts"
	"github.com/Oudwins/droner/pkgs/droner/sdk"
)

type GitHubBranchData struct {
	BranchExists bool
	PullRequest  *GitHubPullRequest
}

type GitHubPullRequest struct {
	Number             int                  `json:"number"`
	State              string               `json:"state"`
	MergedAt           *time.Time           `json:"merged_at,omitempty"`
	ClosedAt           *time.Time           `json:"closed_at,omitempty"`
	CreatedAt          time.Time            `json:"created_at"`
	UpdatedAt          time.Time            `json:"updated_at"`
	Title              string               `json:"title"`
	HTMLURL            string               `json:"html_url"`
	Draft              bool                 `json:"draft"`
	Mergeable          *bool                `json:"mergeable"`
	MergeableState     string               `json:"mergeable_state"`
	RequestedReviewers []GitHubUser         `json:"requested_reviewers"`
	RequestedTeams     []GitHubTeam         `json:"requested_teams"`
	Head               GitHubBranchRef      `json:"head"`
	Base               GitHubBranchRef      `json:"base"`
	Reviews            []GitHubReview       `json:"-"`
	CI                 GitHubCIStatusResult `json:"-"`
}

type GitHubBranchRef struct {
	Ref string `json:"ref"`
	SHA string `json:"sha"`
}

type GitHubUser struct {
	Login string `json:"login"`
}

type GitHubTeam struct {
	Slug string `json:"slug"`
}

type GitHubReview struct {
	User  GitHubUser `json:"user"`
	State string     `json:"state"`
}

type GitHubCombinedStatus struct {
	State    string                `json:"state"`
	Statuses []GitHubStatusContext `json:"statuses"`
}

type GitHubStatusContext struct {
	Context     string `json:"context"`
	State       string `json:"state"`
	Description string `json:"description"`
	TargetURL   string `json:"target_url"`
}

type GitHubCheckRunsResponse struct {
	CheckRuns []GitHubCheckRun `json:"check_runs"`
}

type GitHubCheckRun struct {
	Name       string  `json:"name"`
	Status     string  `json:"status"`
	Conclusion *string `json:"conclusion"`
	HTMLURL    string  `json:"html_url"`
}

type GitHubCIStatusResult struct {
	CombinedStatus GitHubCombinedStatus
	CheckRuns      []GitHubCheckRun
}

type GitHubSDK interface {
	IsAuthenticated() bool
	EnsureAuth() error
	GetBranchData(ctx context.Context, remoteURL string, branch string) (GitHubBranchData, error)
	SetAuthToken(token string)
}

type liveGitHubSDK struct {
	token      string
	mu         sync.RWMutex
	apiBaseURL string
	httpClient *http.Client
}

func newLiveGitHubSDK() *liveGitHubSDK {
	var token string
	if resolvedToken, err := resolveGitHubToken(); err == nil {
		token = resolvedToken
	}
	return &liveGitHubSDK{
		token:      token,
		apiBaseURL: "https://api.github.com",
		httpClient: &http.Client{Timeout: timeouts.SecondDefault},
	}
}

func (s *liveGitHubSDK) SetAuthToken(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.token = strings.TrimSpace(token)
}

func (s *liveGitHubSDK) IsAuthenticated() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
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
	if err := s.EnsureAuth(); err != nil {
		return GitHubBranchData{}, err
	}

	owner, repo, err := parseGitHubURL(remoteURL)
	if err != nil {
		return GitHubBranchData{}, err
	}

	branchExists, err := s.fetchBranchExists(ctx, owner, repo, branch)
	if err != nil {
		return GitHubBranchData{}, err
	}

	pullRequest, err := s.fetchPullRequestForBranch(ctx, remoteURL, owner, repo, branch)
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

func (s *liveGitHubSDK) fetchPullRequestForBranch(ctx context.Context, remoteURL string, owner string, repo string, branch string) (*GitHubPullRequest, error) {
	q := url.Values{}
	q.Set("head", owner+":"+branch)
	q.Set("state", "all")
	q.Set("per_page", "100")

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

	pull := newestPullRequest(pulls)
	detail, err := s.fetchPullRequestDetail(ctx, owner, repo, pull.Number)
	if err != nil {
		return nil, err
	}
	detail.Reviews, err = s.fetchPullRequestReviews(ctx, owner, repo, detail.Number)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(detail.Head.SHA) != "" {
		detail.CI, err = s.fetchCIStatus(ctx, owner, repo, detail.Head.SHA)
		if err != nil {
			return nil, err
		}
	}
	return &detail, nil
}

func newestPullRequest(pulls []GitHubPullRequest) GitHubPullRequest {
	newest := pulls[0]
	for _, pull := range pulls[1:] {
		if pull.UpdatedAt.After(newest.UpdatedAt) || (pull.UpdatedAt.Equal(newest.UpdatedAt) && pull.Number > newest.Number) {
			newest = pull
		}
	}
	return newest
}

func normalizeGitHubPullRequest(remoteURL, owner, repo string, pull *GitHubPullRequest) *PullRequestSnapshot {
	if pull == nil {
		return nil
	}
	reviewers := make([]string, 0, len(pull.RequestedReviewers))
	for _, reviewer := range pull.RequestedReviewers {
		if login := strings.TrimSpace(reviewer.Login); login != "" {
			reviewers = append(reviewers, login)
		}
	}
	sort.Strings(reviewers)

	teams := make([]string, 0, len(pull.RequestedTeams))
	for _, team := range pull.RequestedTeams {
		if slug := strings.TrimSpace(team.Slug); slug != "" {
			teams = append(teams, slug)
		}
	}
	sort.Strings(teams)

	return &PullRequestSnapshot{
		Provider:           "github",
		RemoteURL:          remoteURL,
		RepoOwner:          owner,
		RepoName:           repo,
		Number:             pull.Number,
		State:              strings.TrimSpace(pull.State),
		Title:              pull.Title,
		HTMLURL:            pull.HTMLURL,
		Draft:              pull.Draft,
		HeadRef:            pull.Head.Ref,
		HeadSHA:            pull.Head.SHA,
		BaseRef:            pull.Base.Ref,
		Mergeable:          pull.Mergeable,
		MergeableState:     pull.MergeableState,
		RequestedReviewers: reviewers,
		RequestedTeams:     teams,
		ReviewSummary:      summarizeReviews(pull.Reviews),
		CI:                 summarizeCI(pull.CI),
		CreatedAt:          pull.CreatedAt.UTC(),
		UpdatedAt:          pull.UpdatedAt.UTC(),
		ClosedAt:           utcTimePtr(pull.ClosedAt),
		MergedAt:           utcTimePtr(pull.MergedAt),
	}
}

func summarizeReviews(reviews []GitHubReview) ReviewSummary {
	approved := map[string]struct{}{}
	changesRequested := map[string]struct{}{}
	commented := map[string]struct{}{}
	for _, review := range reviews {
		login := strings.TrimSpace(review.User.Login)
		if login == "" {
			continue
		}
		switch strings.ToUpper(strings.TrimSpace(review.State)) {
		case "APPROVED":
			approved[login] = struct{}{}
		case "CHANGES_REQUESTED":
			changesRequested[login] = struct{}{}
		case "COMMENTED":
			commented[login] = struct{}{}
		}
	}
	return ReviewSummary{Approved: sortedKeys(approved), ChangesRequested: sortedKeys(changesRequested), Commented: sortedKeys(commented)}
}

func summarizeCI(ci GitHubCIStatusResult) CIStatusSummary {
	statuses := make([]CIStatusContext, 0, len(ci.CombinedStatus.Statuses)+len(ci.CheckRuns))
	for _, status := range ci.CombinedStatus.Statuses {
		name := strings.TrimSpace(status.Context)
		if name == "" {
			continue
		}
		statuses = append(statuses, CIStatusContext{Name: name, State: normalizeCIState(status.State), Description: status.Description, TargetURL: status.TargetURL})
	}
	for _, check := range ci.CheckRuns {
		name := strings.TrimSpace(check.Name)
		if name == "" {
			continue
		}
		state := check.Status
		if check.Conclusion != nil && strings.TrimSpace(*check.Conclusion) != "" {
			state = *check.Conclusion
		}
		statuses = append(statuses, CIStatusContext{Name: name, State: normalizeCIState(state), TargetURL: check.HTMLURL})
	}
	sort.Slice(statuses, func(i, j int) bool { return statuses[i].Name < statuses[j].Name })
	return CIStatusSummary{State: aggregateCIState(statuses, ci.CombinedStatus.State), Statuses: statuses}
}

func normalizeCIState(state string) string {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "success", "neutral", "skipped":
		return "passing"
	case "failure", "error", "cancelled", "timed_out", "action_required":
		return "failing"
	case "pending", "queued", "in_progress", "requested", "waiting":
		return "pending"
	default:
		return "unknown"
	}
}

func aggregateCIState(statuses []CIStatusContext, combinedState string) string {
	if len(statuses) == 0 {
		return normalizeCIState(combinedState)
	}
	state := "passing"
	for _, status := range statuses {
		switch status.State {
		case "failing":
			return "failing"
		case "pending":
			state = "pending"
		case "unknown":
			if state == "passing" {
				state = "unknown"
			}
		}
	}
	return state
}

func sortedKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func utcTimePtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	utc := value.UTC()
	return &utc
}

func (s *liveGitHubSDK) fetchPullRequestDetail(ctx context.Context, owner string, repo string, number int) (GitHubPullRequest, error) {
	requestURL := fmt.Sprintf("%s/repos/%s/%s/pulls/%d", s.apiBaseURL, owner, repo, number)
	status, body, err := s.doGET(ctx, requestURL)
	if err != nil {
		return GitHubPullRequest{}, err
	}
	if status != http.StatusOK {
		return GitHubPullRequest{}, fmt.Errorf("unexpected github status fetching pull: %d", status)
	}
	var pull GitHubPullRequest
	if err := json.Unmarshal(body, &pull); err != nil {
		return GitHubPullRequest{}, fmt.Errorf("failed to parse github pull response: %w", err)
	}
	return pull, nil
}

func (s *liveGitHubSDK) fetchPullRequestReviews(ctx context.Context, owner string, repo string, number int) ([]GitHubReview, error) {
	q := url.Values{}
	q.Set("per_page", "100")
	requestURL := fmt.Sprintf("%s/repos/%s/%s/pulls/%d/reviews?%s", s.apiBaseURL, owner, repo, number, q.Encode())
	status, body, err := s.doGET(ctx, requestURL)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("unexpected github status listing pull reviews: %d", status)
	}
	var reviews []GitHubReview
	if err := json.Unmarshal(body, &reviews); err != nil {
		return nil, fmt.Errorf("failed to parse github pull reviews response: %w", err)
	}
	return reviews, nil
}

func (s *liveGitHubSDK) fetchCIStatus(ctx context.Context, owner string, repo string, sha string) (GitHubCIStatusResult, error) {
	statusURL := fmt.Sprintf("%s/repos/%s/%s/commits/%s/status", s.apiBaseURL, owner, repo, url.PathEscape(sha))
	status, body, err := s.doGET(ctx, statusURL)
	if err != nil {
		return GitHubCIStatusResult{}, err
	}
	if status != http.StatusOK {
		return GitHubCIStatusResult{}, fmt.Errorf("unexpected github status fetching commit status: %d", status)
	}
	var combined GitHubCombinedStatus
	if err := json.Unmarshal(body, &combined); err != nil {
		return GitHubCIStatusResult{}, fmt.Errorf("failed to parse github commit status response: %w", err)
	}

	checksURL := fmt.Sprintf("%s/repos/%s/%s/commits/%s/check-runs?per_page=100", s.apiBaseURL, owner, repo, url.PathEscape(sha))
	status, body, err = s.doGET(ctx, checksURL)
	if err != nil {
		return GitHubCIStatusResult{}, err
	}
	if status != http.StatusOK {
		return GitHubCIStatusResult{}, fmt.Errorf("unexpected github status fetching check runs: %d", status)
	}
	var checks GitHubCheckRunsResponse
	if err := json.Unmarshal(body, &checks); err != nil {
		return GitHubCIStatusResult{}, fmt.Errorf("failed to parse github check runs response: %w", err)
	}
	return GitHubCIStatusResult{CombinedStatus: combined, CheckRuns: checks.CheckRuns}, nil
}

func (s *liveGitHubSDK) doGET(ctx context.Context, requestURL string) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "droner")

	s.mu.RLock()
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(s.token))
	s.mu.RUnlock()

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

func resolveGitHubToken() (string, error) {
	store, err := auth.Default()
	if err != nil {
		return "", err
	}

	if githubAuth, ok := store.GitHub(); ok {
		if token := strings.TrimSpace(githubAuth.AccessToken); token != "" {
			return token, nil
		}
	}

	return "", sdk.ErrAuthRequired
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
