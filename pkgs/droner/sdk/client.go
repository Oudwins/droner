package sdk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Oudwins/droner/pkgs/droner/internals/env"
	"github.com/Oudwins/droner/pkgs/droner/internals/schemas"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
}

var ErrAuthRequired = errors.New("auth required")
var ErrShutdownUnsupported = errors.New("shutdown unsupported")

type ErrorResponse struct {
	Status  string              `json:"status"`
	Code    string              `json:"code"`
	Message string              `json:"message"`
	Errors  map[string][]string `json:"errors,omitempty"`
}

type APIError struct {
	StatusCode int
	Code       string
	Message    string
}

func (e *APIError) Error() string {
	if e.Code != "" && e.Message != "" {
		return fmt.Sprintf("%s: %s", e.Code, e.Message)
	}
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("unexpected status: %d", e.StatusCode)
}

type Option func(*Client)

func WithBaseURL(baseURL string) Option {
	return func(c *Client) {
		c.baseURL = strings.TrimRight(baseURL, "/")
	}
}

func WithHTTPClient(client *http.Client) Option {
	return func(c *Client) {
		if client != nil {
			c.httpClient = client
		}
	}
}

func NewClient(opts ...Option) *Client {
	envs := env.Get()
	client := &Client{
		baseURL: strings.TrimRight(envs.BASE_URL, "/"),
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
	for _, opt := range opts {
		opt(client)
	}
	return client
}

func (c *Client) Version(ctx context.Context) (string, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/version", nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", responseError(resp)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(body)), nil
}

func (c *Client) Shutdown(ctx context.Context) error {
	resp, err := c.doRequest(ctx, http.MethodPost, "/shutdown", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return nil
	}
	if resp.StatusCode == http.StatusNotFound {
		return ErrShutdownUnsupported
	}
	return responseError(resp)
}

func (c *Client) CreateSession(ctx context.Context, request schemas.SessionCreateRequest) (*schemas.SessionCreateResponse, error) {
	body, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}

	resp, err := c.doRequest(ctx, http.MethodPost, "/sessions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return nil, responseError(resp)
	}

	var payload schemas.SessionCreateResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}

	return &payload, nil
}

func (c *Client) DeleteSession(ctx context.Context, request schemas.SessionDeleteRequest) (*schemas.TaskResponse, error) {
	body, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}

	resp, err := c.doRequest(ctx, http.MethodDelete, "/sessions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return nil, responseError(resp)
	}

	var payload schemas.TaskResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}

	return &payload, nil
}

func (c *Client) NukeSessions(ctx context.Context) (*schemas.TaskResponse, error) {
	resp, err := c.doRequest(ctx, http.MethodPost, "/sessions/nuke", bytes.NewReader([]byte("{}")))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return nil, responseError(resp)
	}

	var payload schemas.TaskResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}

	return &payload, nil
}

func (c *Client) CompleteSession(ctx context.Context, request schemas.SessionCompleteRequest) (*schemas.TaskResponse, error) {
	body, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}

	resp, err := c.doRequest(ctx, http.MethodPost, "/sessions/complete", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return nil, responseError(resp)
	}

	var payload schemas.TaskResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}

	return &payload, nil
}

func (c *Client) ListSessions(ctx context.Context) (*schemas.SessionListResponse, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/sessions", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, responseError(resp)
	}

	var payload schemas.SessionListResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}

	return &payload, nil
}

func (c *Client) ListSessionsAll(ctx context.Context) (*schemas.SessionListResponse, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/sessions?all=1", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, responseError(resp)
	}

	var payload schemas.SessionListResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}

	return &payload, nil
}

func (c *Client) TaskStatus(ctx context.Context, taskID string) (*schemas.TaskResponse, error) {
	path := "/tasks/" + url.PathEscape(taskID)
	resp, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, responseError(resp)
	}

	var payload schemas.TaskResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}

	return &payload, nil
}

func (c *Client) doRequest(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.httpClient.Do(req)
}

func responseError(resp *http.Response) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("unexpected status: %s", resp.Status)
	}

	var payload ErrorResponse
	if err := json.Unmarshal(body, &payload); err == nil {
		if payload.Code == "auth_required" {
			return ErrAuthRequired
		}
		return &APIError{StatusCode: resp.StatusCode, Code: payload.Code, Message: payload.Message}
	}

	return fmt.Errorf("unexpected status: %s", resp.Status)
}

type GitHubOAuthStartResponse struct {
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete,omitempty"`
	UserCode                string `json:"user_code"`
	State                   string `json:"state"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

type GitHubOAuthStatusResponse struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

func (c *Client) StartGitHubOAuth(ctx context.Context) (*GitHubOAuthStartResponse, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/oauth/github/start", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, responseError(resp)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var payload GitHubOAuthStartResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}

	if payload.VerificationURI == "" && payload.VerificationURIComplete == "" {
		var fallback map[string]any
		if err := json.Unmarshal(body, &fallback); err == nil {
			if authURL, ok := fallback["auth_url"].(string); ok && authURL != "" {
				payload.VerificationURIComplete = authURL
			}
			if state, ok := fallback["state"].(string); ok && payload.State == "" {
				payload.State = state
			}
		}
	}

	if payload.VerificationURIComplete == "" && payload.VerificationURI != "" && payload.UserCode != "" {
		payload.VerificationURIComplete = payload.VerificationURI + "?user_code=" + url.QueryEscape(payload.UserCode)
	}

	return &payload, nil
}

func (c *Client) GitHubOAuthStatus(ctx context.Context, state string) (*GitHubOAuthStatusResponse, error) {
	path := "/oauth/github/status?state=" + url.QueryEscape(state)
	resp, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, responseError(resp)
	}

	var payload GitHubOAuthStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	return &payload, nil
}
