package sdk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Oudwins/droner/pkgs/droner/internals/env"
	"github.com/Oudwins/droner/pkgs/droner/internals/schemas"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
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
		return "", unexpectedStatusError(resp)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(body)), nil
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

	if resp.StatusCode != http.StatusOK {
		return nil, unexpectedStatusError(resp)
	}

	var payload schemas.SessionCreateResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}

	return &payload, nil
}

func (c *Client) DeleteSession(ctx context.Context, request schemas.SessionDeleteRequest) (*schemas.SessionDeleteResponse, error) {
	body, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}

	resp, err := c.doRequest(ctx, http.MethodDelete, "/sessions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, unexpectedStatusError(resp)
	}

	var payload schemas.SessionDeleteResponse
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

func unexpectedStatusError(resp *http.Response) error {
	return fmt.Errorf("unexpected status: %s", resp.Status)
}
