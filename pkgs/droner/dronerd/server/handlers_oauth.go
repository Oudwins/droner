package server

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Oudwins/droner/pkgs/droner/internals/auth"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"
)

const (
	githubOAuthClientID = "Iv23lipXEtXtcPl3A2B3"
	githubOAuthScope    = "repo"
	oauthStateTTL       = 10 * time.Minute
)

var now = time.Now

var oauthHTTPClient = &http.Client{Timeout: 10 * time.Second}

type oauthStateStatus string

const (
	oauthStatusPending  oauthStateStatus = "pending"
	oauthStatusComplete oauthStateStatus = "complete"
	oauthStatusFailed   oauthStateStatus = "failed"
)

type oauthState struct {
	deviceCode string
	interval   time.Duration
	nextPoll   time.Time
	expiresAt  time.Time
	status     oauthStateStatus
	err        string
	createdAt  time.Time
}

type oauthStateStore struct {
	mu     sync.RWMutex
	states map[string]*oauthState
}

type oauthStartResponse struct {
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete,omitempty"`
	UserCode                string `json:"user_code"`
	State                   string `json:"state"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

type oauthStatusResponse struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

type githubDeviceCodeResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
	Error                   string `json:"error"`
	ErrorDescription        string `json:"error_description"`
}

type githubTokenResponse struct {
	AccessToken      string `json:"access_token"`
	TokenType        string `json:"token_type"`
	Scope            string `json:"scope"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

func newOAuthStateStore() *oauthStateStore {
	return &oauthStateStore{states: make(map[string]*oauthState)}
}

func (s *oauthStateStore) create(deviceCode string, interval time.Duration, expiresAt time.Time) (string, error) {
	for range 5 {
		state, err := randomString(16)
		if err != nil {
			return "", err
		}
		s.mu.Lock()
		if _, exists := s.states[state]; !exists {
			s.states[state] = &oauthState{
				deviceCode: deviceCode,
				interval:   interval,
				nextPoll:   now().Add(interval),
				expiresAt:  expiresAt,
				status:     oauthStatusPending,
				createdAt:  now(),
			}
			s.mu.Unlock()
			return state, nil
		}
		s.mu.Unlock()
	}
	state, err := randomString(16)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	s.states[state] = &oauthState{
		deviceCode: deviceCode,
		interval:   interval,
		nextPoll:   now().Add(interval),
		expiresAt:  expiresAt,
		status:     oauthStatusPending,
		createdAt:  now(),
	}
	s.mu.Unlock()
	return state, nil
}

func (s *oauthStateStore) status(state string) (oauthStateStatus, string, bool) {
	s.mu.RLock()
	record, exists := s.states[state]
	s.mu.RUnlock()
	if !exists {
		return oauthStatusFailed, "unknown_state", false
	}

	if now().Sub(record.createdAt) > oauthStateTTL {
		s.mu.Lock()
		record.status = oauthStatusFailed
		record.err = "expired"
		s.mu.Unlock()
	}

	s.mu.RLock()
	status := record.status
	err := record.err
	s.mu.RUnlock()
	return status, err, true
}

func (s *oauthStateStore) get(state string) (*oauthState, bool) {
	s.mu.RLock()
	record, exists := s.states[state]
	s.mu.RUnlock()
	if !exists {
		return nil, false
	}
	return record, true
}

func (s *oauthStateStore) mark(state string, status oauthStateStatus, err string) {
	s.mu.Lock()
	if record, exists := s.states[state]; exists {
		record.status = status
		record.err = err
	}
	s.mu.Unlock()
}

func (s *oauthStateStore) updatePoll(state string, interval time.Duration) {
	s.mu.Lock()
	if record, exists := s.states[state]; exists {
		record.interval = interval
		record.nextPoll = now().Add(interval)
	}
	s.mu.Unlock()
}

func (s *Server) HandlerGitHubOAuthStart(_ *slog.Logger, w http.ResponseWriter, r *http.Request) {
	config := githubOAuthConfig()
	deviceResp, err := s.requestGitHubDeviceCode(r, config)
	if err != nil {
		RenderJSON(w, r, JsonResponseError(JsonResponseErroCodeInternal, err.Error(), nil), Render.Status(http.StatusInternalServerError))
		return
	}

	interval := time.Duration(deviceResp.Interval) * time.Second
	expiresAt := now().Add(time.Duration(deviceResp.ExpiresIn) * time.Second)
	state, err := s.oauth.create(deviceResp.DeviceCode, interval, expiresAt)
	if err != nil {
		RenderJSON(w, r, JsonResponseError(JsonResponseErroCodeInternal, "Failed to create auth state", nil), Render.Status(http.StatusInternalServerError))
		return
	}

	RenderJSON(w, r, oauthStartResponse{
		VerificationURI:         deviceResp.VerificationURI,
		VerificationURIComplete: deviceResp.VerificationURIComplete,
		UserCode:                deviceResp.UserCode,
		State:                   state,
		ExpiresIn:               deviceResp.ExpiresIn,
		Interval:                deviceResp.Interval,
	})
}

func (s *Server) HandlerGitHubOAuthStatus(_ *slog.Logger, w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	if state == "" {
		RenderJSON(w, r, oauthStatusResponse{Status: string(oauthStatusFailed), Error: "missing_state"})
		return
	}

	status, errMsg, exists := s.oauth.status(state)
	if !exists {
		RenderJSON(w, r, oauthStatusResponse{Status: string(oauthStatusFailed), Error: errMsg})
		return
	}
	if status != oauthStatusPending {
		RenderJSON(w, r, oauthStatusResponse{Status: string(status), Error: errMsg})
		return
	}

	record, ok := s.oauth.get(state)
	if !ok {
		RenderJSON(w, r, oauthStatusResponse{Status: string(oauthStatusFailed), Error: "unknown_state"})
		return
	}

	if now().After(record.expiresAt) {
		s.oauth.mark(state, oauthStatusFailed, "expired")
		RenderJSON(w, r, oauthStatusResponse{Status: string(oauthStatusFailed), Error: "expired"})
		return
	}

	if now().Before(record.nextPoll) {
		RenderJSON(w, r, oauthStatusResponse{Status: string(oauthStatusPending)})
		return
	}

	config := githubOAuthConfig()
	result, err := s.exchangeGitHubDeviceToken(r, config, record.deviceCode)
	if err != nil {
		s.oauth.mark(state, oauthStatusFailed, err.Error())
		RenderJSON(w, r, oauthStatusResponse{Status: string(oauthStatusFailed), Error: err.Error()})
		return
	}

	switch result.Status {
	case oauthStatusComplete:
		if err := auth.WriteGitHubAuth(auth.GitHubAuth{
			AccessToken: result.AccessToken,
			TokenType:   result.TokenType,
			Scope:       result.Scope,
			UpdatedAt:   now().UTC(),
		}); err != nil {
			s.oauth.mark(state, oauthStatusFailed, "failed_to_store_token")
			RenderJSON(w, r, oauthStatusResponse{Status: string(oauthStatusFailed), Error: "failed_to_store_token"})
			return
		}
		s.oauth.mark(state, oauthStatusComplete, "")
		RenderJSON(w, r, oauthStatusResponse{Status: string(oauthStatusComplete)})
		return
	case oauthStatusPending:
		if result.Interval > 0 {
			s.oauth.updatePoll(state, result.Interval)
		}
		RenderJSON(w, r, oauthStatusResponse{Status: string(oauthStatusPending)})
		return
	default:
		s.oauth.mark(state, oauthStatusFailed, result.Error)
		RenderJSON(w, r, oauthStatusResponse{Status: string(oauthStatusFailed), Error: result.Error})
		return
	}
}

type deviceTokenResult struct {
	Status      oauthStateStatus
	AccessToken string
	TokenType   string
	Scope       string
	Interval    time.Duration
	Error       string
}

func (s *Server) requestGitHubDeviceCode(r *http.Request, config *oauth2.Config) (*githubDeviceCodeResponse, error) {
	values := url.Values{}
	values.Set("client_id", config.ClientID)
	values.Set("scope", strings.Join(config.Scopes, " "))

	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, githubDeviceCodeEndpoint(config), strings.NewReader(values.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := oauthHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var payload githubDeviceCodeResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}

	if payload.Error != "" {
		if payload.ErrorDescription != "" {
			return nil, fmt.Errorf("%s", payload.ErrorDescription)
		}
		return nil, fmt.Errorf("%s", payload.Error)
	}

	if payload.DeviceCode == "" || payload.UserCode == "" || payload.VerificationURI == "" {
		return nil, fmt.Errorf("invalid device code response")
	}

	if payload.VerificationURIComplete == "" {
		payload.VerificationURIComplete = prefillDeviceURL(payload.VerificationURI, payload.UserCode)
	}

	if payload.Interval == 0 {
		payload.Interval = 5
	}

	return &payload, nil
}

func (s *Server) exchangeGitHubDeviceToken(r *http.Request, config *oauth2.Config, deviceCode string) (*deviceTokenResult, error) {
	values := url.Values{}
	values.Set("client_id", config.ClientID)
	values.Set("device_code", deviceCode)
	values.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")

	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, config.Endpoint.TokenURL, strings.NewReader(values.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := oauthHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var tokenResp githubTokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, err
	}

	if tokenResp.AccessToken != "" {
		return &deviceTokenResult{
			Status:      oauthStatusComplete,
			AccessToken: tokenResp.AccessToken,
			TokenType:   tokenResp.TokenType,
			Scope:       tokenResp.Scope,
		}, nil
	}

	if tokenResp.Error == "" {
		return &deviceTokenResult{Status: oauthStatusFailed, Error: "unknown_error"}, nil
	}

	switch tokenResp.Error {
	case "authorization_pending":
		return &deviceTokenResult{Status: oauthStatusPending}, nil
	case "slow_down":
		return &deviceTokenResult{Status: oauthStatusPending, Interval: 5 * time.Second}, nil
	case "expired_token":
		return &deviceTokenResult{Status: oauthStatusFailed, Error: "expired"}, nil
	case "access_denied":
		return &deviceTokenResult{Status: oauthStatusFailed, Error: "access_denied"}, nil
	default:
		if tokenResp.ErrorDescription != "" {
			return &deviceTokenResult{Status: oauthStatusFailed, Error: tokenResp.ErrorDescription}, nil
		}
		return &deviceTokenResult{Status: oauthStatusFailed, Error: tokenResp.Error}, nil
	}
}

func randomString(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func prefillDeviceURL(verificationURI string, userCode string) string {
	if verificationURI == "" || userCode == "" {
		return ""
	}
	parsed, err := url.Parse(verificationURI)
	if err != nil {
		return verificationURI + "?user_code=" + url.QueryEscape(userCode)
	}
	query := parsed.Query()
	if query.Get("user_code") == "" {
		query.Set("user_code", userCode)
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func githubOAuthConfig() *oauth2.Config {
	return &oauth2.Config{
		ClientID: githubOAuthClientID,
		Endpoint: github.Endpoint,
		Scopes:   []string{githubOAuthScope},
	}
}

func githubDeviceCodeEndpoint(config *oauth2.Config) string {
	if config != nil && config.Endpoint.AuthURL != "" {
		trimmed := strings.TrimSuffix(config.Endpoint.AuthURL, "/oauth/authorize")
		if trimmed != "" {
			return trimmed + "/device/code"
		}
	}
	return "https://github.com/login/device/code"
}
