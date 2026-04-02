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
	"strconv"
	"strings"

	"github.com/Oudwins/droner/pkgs/droner/internals/env"
	"github.com/Oudwins/droner/pkgs/droner/internals/schemas"
	"github.com/Oudwins/droner/pkgs/droner/internals/timeouts"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
}

// SessionStatus represents the public lifecycle state of a session.
type SessionStatus string

const (
	SessionStatusQueued     SessionStatus = "queued"
	SessionStatusRunning    SessionStatus = "running"
	SessionStatusCompleting SessionStatus = "completing"
	SessionStatusDeleted    SessionStatus = "deleted"
	SessionStatusCompleted  SessionStatus = "completed"
)

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
			Timeout: timeouts.SecondLong,
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
	if resp.StatusCode == http.StatusAccepted {
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
	// Request without status filter (alias)
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

// ListSessionsWithParams requests sessions with optional statuses and pagination.
// If statuses is nil or empty, no status filter is applied.
func (c *Client) ListSessionsWithParams(ctx context.Context, statuses []string, limit int, cursor string) (*schemas.SessionListResponse, error) {
	path := "/sessions"
	q := make([]string, 0)
	if len(statuses) > 0 {
		for _, s := range statuses {
			q = append(q, "status="+url.QueryEscape(s))
		}
	}
	if limit > 0 {
		q = append(q, "limit="+strconv.Itoa(limit))
	}
	if cursor != "" {
		q = append(q, "cursor="+url.QueryEscape(cursor))
	}
	if len(q) > 0 {
		path = path + "?" + strings.Join(q, "&")
	}

	resp, err := c.doRequest(ctx, http.MethodGet, path, nil)
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
