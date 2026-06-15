package backends

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/Oudwins/droner/pkgs/droner/internals/conf"
	"github.com/Oudwins/droner/pkgs/droner/internals/messages"
	"github.com/Oudwins/droner/pkgs/droner/internals/timeouts"
	opencode "github.com/sst/opencode-sdk-go"
	"github.com/sst/opencode-sdk-go/option"
)

type opencodeClient struct {
	config  conf.OpenCodeConfig
	sdk     *opencode.Client
	http    *http.Client
	baseURL string
}

func newOpencodeClient(config conf.OpenCodeConfig) *opencodeClient {
	baseURL := fmt.Sprintf("http://%s:%d", config.Hostname, config.Port)
	return &opencodeClient{
		config:  config,
		baseURL: baseURL,
		sdk:     opencode.NewClient(option.WithBaseURL(baseURL)),
		http:    &http.Client{Timeout: timeouts.SecondLong},
	}
}

func (c *opencodeClient) CreateSession(ctx context.Context, worktreePath string) (string, error) {
	params := opencode.SessionNewParams{}
	if strings.TrimSpace(worktreePath) != "" {
		params.Directory = opencode.F(worktreePath)
	}
	session, err := c.sdk.Session.New(ctx, params, option.WithRequestTimeout(timeouts.SecondLong))
	if err != nil {
		return "", err
	}
	if session == nil || strings.TrimSpace(session.ID) == "" {
		return "", errors.New("opencode session id missing from response")
	}
	return session.ID, nil
}

func (c *opencodeClient) LatestSessionID(ctx context.Context, worktreePath string) (string, error) {
	params := opencode.SessionListParams{}
	if strings.TrimSpace(worktreePath) != "" {
		params.Directory = opencode.F(worktreePath)
	}
	sessions, err := c.sdk.Session.List(ctx, params, option.WithRequestTimeout(timeouts.SecondLong))
	if err != nil {
		return "", err
	}
	if sessions == nil || len(*sessions) == 0 {
		return "", nil
	}
	return strings.TrimSpace((*sessions)[0].ID), nil
}

func (c *opencodeClient) SendPrompt(ctx context.Context, sessionID string, directory string, model string, agentName string, message *messages.Message, noReply bool) error {
	if message == nil || len(message.Parts) == 0 {
		return nil
	}
	parts, err := opencodePartsFromMessage(message, directory)
	if err != nil {
		return err
	}
	if len(parts) == 0 {
		return nil
	}
	params := opencode.SessionPromptParams{Parts: opencode.F(parts)}
	if noReply {
		params.NoReply = opencode.F(true)
	}
	if strings.TrimSpace(directory) != "" {
		params.Directory = opencode.F(directory)
	}
	if strings.TrimSpace(agentName) != "" {
		params.Agent = opencode.F(strings.TrimSpace(agentName))
	}
	if providerID, modelID, ok := parseOpencodeModel(model); ok {
		params.Model = opencode.F(opencode.SessionPromptParamsModel{
			ProviderID: opencode.F(providerID),
			ModelID:    opencode.F(modelID),
		})
	}
	if strings.TrimSpace(sessionID) == "" {
		id, err := c.CreateSession(ctx, "")
		if err != nil {
			return err
		}
		sessionID = id
	}
	payload, err := json.Marshal(params)
	if err != nil {
		return err
	}
	endpoint := fmt.Sprintf("%s/session/%s/prompt_async", c.baseURL, sessionID)
	if query := params.URLQuery(); len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 8*1024))
		if len(bodyBytes) == 0 {
			return fmt.Errorf("opencode prompt request failed: %s", resp.Status)
		}
		return fmt.Errorf("opencode prompt request failed: %s: %s", resp.Status, strings.TrimSpace(string(bodyBytes)))
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

func (c *opencodeClient) SendCommand(ctx context.Context, sessionID string, directory string, model string, agentName string, command *messages.CommandInvocation) error {
	if command == nil || !command.HasContent() {
		return nil
	}
	if strings.TrimSpace(sessionID) == "" {
		id, err := c.CreateSession(ctx, "")
		if err != nil {
			return err
		}
		sessionID = id
	}
	if len(command.Parts) > 0 {
		parts := make([]messages.MessagePart, 0, len(command.Parts)+1)
		if text := command.InvocationText(); strings.TrimSpace(text) != "" {
			parts = append(parts, messages.NewTextPart(text))
		}
		parts = append(parts, command.Parts...)
		return c.SendPrompt(ctx, sessionID, directory, model, agentName, &messages.Message{Parts: parts}, false)
	}
	params := opencode.SessionCommandParams{
		Command:   opencode.F(strings.TrimSpace(command.Name)),
		Arguments: opencode.F(command.Arguments),
	}
	if strings.TrimSpace(directory) != "" {
		params.Directory = opencode.F(directory)
	}
	if strings.TrimSpace(agentName) != "" {
		params.Agent = opencode.F(strings.TrimSpace(agentName))
	}
	if strings.TrimSpace(model) != "" {
		params.Model = opencode.F(strings.TrimSpace(model))
	}
	_, err := c.sdk.Session.Command(ctx, sessionID, params, option.WithRequestTimeout(timeouts.SecondLong))
	return err
}
