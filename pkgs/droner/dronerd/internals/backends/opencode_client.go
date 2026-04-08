package backends

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
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

type opencodeCommandRequest struct {
	Command   string                    `json:"command"`
	Arguments string                    `json:"arguments,omitempty"`
	Agent     string                    `json:"agent,omitempty"`
	Model     string                    `json:"model,omitempty"`
	Parts     []opencodeCommandFilePart `json:"parts,omitempty"`
}

type opencodeCommandFilePart struct {
	Type     string                     `json:"type"`
	URL      string                     `json:"url,omitempty"`
	Mime     string                     `json:"mime,omitempty"`
	Filename string                     `json:"filename,omitempty"`
	Source   *opencodeCommandFileSource `json:"source,omitempty"`
}

type opencodeCommandFileSource struct {
	Type string                       `json:"type"`
	Path string                       `json:"path"`
	Text opencodeCommandFileSourceRef `json:"text"`
}

type opencodeCommandFileSourceRef struct {
	Start int64  `json:"start"`
	End   int64  `json:"end"`
	Value string `json:"value"`
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
	_, err = c.sdk.Session.Prompt(ctx, sessionID, params, option.WithRequestTimeout(timeouts.SecondLong))
	return err
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
	parts, err := opencodeCommandPartsFromCommand(command, directory)
	if err != nil {
		return err
	}
	body := opencodeCommandRequest{
		Command:   strings.TrimSpace(command.Name),
		Arguments: command.Arguments,
		Agent:     strings.TrimSpace(agentName),
		Parts:     parts,
	}
	if strings.TrimSpace(model) != "" {
		body.Model = strings.TrimSpace(model)
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	endpoint := fmt.Sprintf("%s/session/%s/command", c.baseURL, sessionID)
	if strings.TrimSpace(directory) != "" {
		query := url.Values{}
		query.Set("directory", strings.TrimSpace(directory))
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
			return fmt.Errorf("opencode command request failed: %s", resp.Status)
		}
		return fmt.Errorf("opencode command request failed: %s: %s", resp.Status, strings.TrimSpace(string(bodyBytes)))
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

func opencodeCommandPartsFromCommand(command *messages.CommandInvocation, worktreePath string) ([]opencodeCommandFilePart, error) {
	if command == nil || len(command.Parts) == 0 {
		return nil, nil
	}
	parts := make([]opencodeCommandFilePart, 0, len(command.Parts))
	for _, part := range command.Parts {
		if part.Type != messages.PartTypeFile {
			continue
		}
		filePart, err := opencodeCommandFilePartFromMessagePart(part, worktreePath)
		if err != nil {
			return nil, err
		}
		parts = append(parts, filePart)
	}
	return parts, nil
}

func opencodeCommandFilePartFromMessagePart(part messages.MessagePart, worktreePath string) (opencodeCommandFilePart, error) {
	if part.File == nil {
		return opencodeCommandFilePart{}, errors.New("file message part is missing file payload")
	}
	filePart := opencodeCommandFilePart{
		Type:     "file",
		Mime:     strings.TrimSpace(part.File.Mime),
		Filename: strings.TrimSpace(part.File.Filename),
	}
	inlineURL := ""
	if part.File.URL != nil {
		inlineURL = strings.TrimSpace(*part.File.URL)
	}
	if inlineURL != "" {
		if filePart.Mime == "" {
			return opencodeCommandFilePart{}, errors.New("inline file message part is missing mime type")
		}
		if filePart.Filename == "" {
			return opencodeCommandFilePart{}, errors.New("inline file message part is missing filename")
		}
		filePart.URL = inlineURL
		return filePart, nil
	}
	if part.File.Source == nil {
		return opencodeCommandFilePart{}, errors.New("file message part is missing file source")
	}
	if strings.TrimSpace(worktreePath) == "" {
		return opencodeCommandFilePart{}, errors.New("worktree path is required for file message parts")
	}
	relativePath := part.File.Source.Path
	absolutePath := filepath.Join(worktreePath, relativePath)
	if _, err := os.Stat(absolutePath); err != nil {
		return opencodeCommandFilePart{}, fmt.Errorf("resolve file part %q: %w", relativePath, err)
	}
	fileURL, err := localFileURL(absolutePath)
	if err != nil {
		return opencodeCommandFilePart{}, fmt.Errorf("resolve file part %q url: %w", relativePath, err)
	}
	if filePart.Filename == "" {
		filePart.Filename = filepath.Base(relativePath)
	}
	if filePart.Mime == "" {
		filePart.Mime = mimeTypeForPath(relativePath)
	}
	filePart.URL = fileURL
	filePart.Source = &opencodeCommandFileSource{
		Type: "file",
		Path: relativePath,
		Text: opencodeCommandFileSourceRef{},
	}
	if part.File.Source.Text != nil {
		filePart.Source.Text = opencodeCommandFileSourceRef{
			Start: part.File.Source.Text.Start,
			End:   part.File.Source.Text.End,
			Value: part.File.Source.Text.Value,
		}
	}
	return filePart, nil
}
