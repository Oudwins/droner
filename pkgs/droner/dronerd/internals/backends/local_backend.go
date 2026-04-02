package backends

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Oudwins/droner/pkgs/droner/dronerd/db"
	"github.com/Oudwins/droner/pkgs/droner/internals/conf"
	"github.com/Oudwins/droner/pkgs/droner/internals/messages"
	"github.com/Oudwins/droner/pkgs/droner/internals/timeouts"
	opencode "github.com/sst/opencode-sdk-go"
)

type commandFunc func(name string, args ...string) *exec.Cmd

var execCommand commandFunc = exec.Command

var opencodeAutorunTimeout = timeouts.DefaultMinutes

func (l LocalBackend) WorktreePath(repoPath string, sessionID string) (string, error) {
	if strings.TrimSpace(sessionID) == "" {
		return "", errors.New("session id is required")
	}
	repoName := filepath.Base(repoPath)
	worktreeName := fmt.Sprintf("%s..%s", repoName, sessionID)
	return filepath.Join(l.config.WorktreeDir, worktreeName), nil
}

func tmuxSessionName(repoPath string, sessionID string) string {
	repoName := filepath.Base(repoPath)
	return fmt.Sprintf("%s#%s", repoName, sessionID)
}

func tmuxSessionNameFromWorktreePath(worktreePath string) string {
	worktreeName := filepath.Base(worktreePath)
	parts := strings.Split(worktreeName, "..")
	if len(parts) != 2 {
		return worktreeName
	}
	return fmt.Sprintf("%s#%s", parts[0], parts[1])
}

func (l LocalBackend) ValidateSessionID(repoPath string, sessionID string) error {
	worktreePath, err := l.WorktreePath(repoPath, sessionID)
	if err != nil {
		return err
	}
	if _, err := os.Stat(worktreePath); err == nil {
		return errors.New("session folder already exists")
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to stat worktree path: %w", err)
	}
	return nil
}

func (l LocalBackend) HydrateSession(ctx context.Context, session db.Session, agentConfig AgentConfig) (HydrationResult, error) {
	sessionName := tmuxSessionName(session.RepoPath, session.Branch)
	if strings.TrimSpace(session.RepoPath) == "" || strings.TrimSpace(session.Branch) == "" {
		sessionName = tmuxSessionNameFromWorktreePath(session.WorktreePath)
	}

	exists, err := l.tmuxSessionExists(sessionName)
	if err != nil {
		return HydrationResult{Status: db.SessionStatusFailed, Error: fmt.Sprintf("failed to inspect tmux session: %v", err)}, nil
	}
	if exists {
		return HydrationResult{Status: db.SessionStatusRunning}, nil
	}

	info, err := os.Stat(session.WorktreePath)
	if err != nil {
		if os.IsNotExist(err) {
			return HydrationResult{Status: db.SessionStatusDeleted}, nil
		}
		return HydrationResult{Status: db.SessionStatusFailed, Error: fmt.Sprintf("failed to stat worktree path: %v", err)}, nil
	}
	if !info.IsDir() {
		return HydrationResult{Status: db.SessionStatusFailed, Error: "worktree path is not a directory"}, nil
	}

	if err := l.hydrateLocalRuntime(ctx, sessionName, session.WorktreePath, agentConfig); err != nil {
		return HydrationResult{Status: db.SessionStatusFailed, Error: err.Error()}, nil
	}

	return HydrationResult{Status: db.SessionStatusRunning}, nil
}

func (l LocalBackend) CreateSession(ctx context.Context, repoPath string, worktreePath string, sessionID string, agentConfig AgentConfig, opts ...CreateSessionOptions) (retErr error) {
	sessionName := tmuxSessionName(repoPath, sessionID)
	createOpts := CreateSessionOptions{}
	if len(opts) > 0 {
		createOpts = opts[0]
	}

	defer func() {
		if retErr == nil {
			return
		}

		// Best-effort cleanup. We prefer to leave no partial state behind.
		_ = l.killTmuxSession(sessionName)

		cleanRoot := filepath.Clean(l.config.WorktreeDir)
		cleanWorktree := filepath.Clean(worktreePath)
		if cleanRoot != "" && cleanWorktree != "" {
			if rel, err := filepath.Rel(cleanRoot, cleanWorktree); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
				_ = l.removeGitWorktreeFromRepo(repoPath, cleanWorktree)
				_ = os.RemoveAll(cleanWorktree)
			}
		}

		if commonGitDir, err := l.gitCommonDirFromRepo(repoPath); err == nil {
			_ = l.deleteGitBranch(commonGitDir, sessionID)
		}
	}()

	if err := os.MkdirAll(l.config.WorktreeDir, 0o755); err != nil {
		return fmt.Errorf("failed to create worktree root: %w", err)
	}
	reused := false
	if createOpts.NextReusableWorktree != nil {
		for {
			candidate, err := createOpts.NextReusableWorktree(ctx)
			if err != nil {
				return err
			}
			if candidate == nil {
				break
			}
			reuseCandidate, ok, err := l.tryReuseProvidedWorktree(repoPath, worktreePath, sessionID, *candidate)
			if err != nil {
				return err
			}
			if createOpts.MarkReusableWorktreeDeletion != nil {
				createOpts.MarkReusableWorktreeDeletion(reuseCandidate)
			}
			if ok {
				reused = true
				break
			}
		}
	}
	if !reused {
		if err := l.createGitWorktree(repoPath, worktreePath, sessionID); err != nil {
			return err
		}
	}
	if err := l.runCursorWorktreeSetup(repoPath, worktreePath, sessionID); err != nil {
		return err
	}
	if err := l.createTmuxBaseSession(sessionName, worktreePath); err != nil {
		return err
	}
	opencodeConfig := agentConfig.Opencode
	if err := l.ensureOpencodeServer(ctx, worktreePath, opencodeConfig); err != nil {
		return err
	}
	opencodeSessionID := ""
	var err error
	if opencodeInputHasContent(agentConfig) {
		opencodeSessionID, err = l.createOpencodeSession(ctx, opencodeConfig, worktreePath)
		if err != nil {
			return err
		}
	}
	agentConfig.Opencode = opencodeConfig
	if err := l.createTmuxOpencodeWindow(sessionName, worktreePath, agentConfig, opencodeSessionID); err != nil {
		return err
	}
	if opencodeInputHasContent(agentConfig) {
		cfg := opencodeConfig
		session := opencodeSessionID
		dir := worktreePath
		model := agentConfig.Model
		agentName := agentConfig.AgentName
		message := agentConfig.Message
		command := messages.CloneCommand(agentConfig.Command)
		go func() {
			promptCtx, cancel := context.WithTimeout(context.Background(), opencodeAutorunTimeout)
			defer cancel()
			if err := l.sendOpencodeInitialInput(promptCtx, cfg, session, dir, model, agentName, message, command); err != nil {
				slog.Warn(
					"failed to autorun opencode prompt",
					slog.String("sessionID", sessionID),
					slog.String("opencodeSessionID", session),
					slog.String("error", err.Error()),
				)
			}
		}()
	}
	if err := l.createTmuxTerminalWindow(sessionName, worktreePath); err != nil {
		return err
	}
	return nil
}

func (l LocalBackend) tryReuseProvidedWorktree(repoPath string, worktreePath string, sessionID string, candidate ReusableWorktreeCandidate) (ReusableWorktreeCandidate, bool, error) {
	cleanRepoPath := filepath.Clean(repoPath)
	cleanTarget := filepath.Clean(worktreePath)
	cleanRoot := filepath.Clean(l.config.WorktreeDir)
	oldWorktreePath := filepath.Clean(candidate.WorktreePath)

	if filepath.Clean(candidate.RepoPath) != cleanRepoPath {
		return candidate, false, nil
	}
	if oldWorktreePath == "" || oldWorktreePath == cleanTarget {
		return candidate, false, nil
	}
	rel, relErr := filepath.Rel(cleanRoot, oldWorktreePath)
	if relErr != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return candidate, false, nil
	}
	info, statErr := os.Stat(oldWorktreePath)
	if statErr != nil || !info.IsDir() {
		return candidate, false, nil
	}

	_ = l.killTmuxSession(candidate.Branch)
	if err := l.resetAndCleanWorktree(oldWorktreePath); err != nil {
		return candidate, false, nil
	}
	if err := l.moveGitWorktree(cleanRepoPath, oldWorktreePath, cleanTarget); err != nil {
		return candidate, false, nil
	}
	baseRef, err := l.resolveBaseRef(cleanRepoPath)
	if err != nil {
		return candidate, false, err
	}
	if err := l.checkoutNewBranch(cleanTarget, sessionID, baseRef); err != nil {
		return candidate, false, err
	}

	commonGitDir, err := l.gitCommonDirFromWorktree(cleanTarget)
	if err != nil {
		return candidate, false, err
	}
	if err := l.deleteGitBranch(commonGitDir, candidate.Branch); err != nil {
		return candidate, false, err
	}
	return candidate, true, nil
}

func (l LocalBackend) gitCommonDirFromRepo(repoPath string) (string, error) {
	cmd := execCommand("git", "-C", repoPath, "rev-parse", "--git-common-dir")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to determine git common dir: %s", strings.TrimSpace(string(output)))
	}
	commonDir := strings.TrimSpace(string(output))
	if commonDir == "" {
		return "", errors.New("failed to determine git common dir")
	}
	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Join(repoPath, commonDir)
	}
	return commonDir, nil
}

func (l LocalBackend) removeGitWorktreeFromRepo(repoPath string, worktreePath string) error {
	cmd := execCommand("git", "-C", repoPath, "worktree", "remove", "--force", worktreePath)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to remove worktree: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

func (l LocalBackend) DeleteSession(_ context.Context, worktreePath string, sessionID string) error {
	sessionName := tmuxSessionNameFromWorktreePath(worktreePath)
	if err := l.killTmuxSession(sessionName); err != nil {
		return err
	}
	if strings.TrimSpace(worktreePath) == "" {
		return nil
	}
	if _, err := os.Stat(worktreePath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	commonGitDir, err := l.gitCommonDirFromWorktree(worktreePath)
	if err != nil {
		return err
	}
	if err := l.removeGitWorktree(worktreePath); err != nil {
		return err
	}
	if err := l.deleteGitBranch(commonGitDir, sessionID); err != nil {
		return err
	}
	return nil
}

func (l LocalBackend) CompleteSession(_ context.Context, worktreePath string, sessionID string) error {
	sessionName := ""
	if strings.TrimSpace(worktreePath) != "" {
		sessionName = tmuxSessionNameFromWorktreePath(worktreePath)
	}
	if strings.TrimSpace(sessionName) == "" {
		sessionName = strings.TrimSpace(sessionID)
	}
	if sessionName == "" {
		return nil
	}
	return l.killTmuxSession(sessionName)
}

func (l LocalBackend) createGitWorktree(repoPath string, worktreePath string, branchName string) error {
	cmd := execCommand("git", "-C", repoPath, "worktree", "add", "-b", branchName, worktreePath)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create worktree: %s: %s", err.Error(), strings.TrimSpace(string(output)))
	}
	return nil
}

func (l LocalBackend) removeGitWorktree(worktreePath string) error {
	cmd := execCommand("git", "-C", worktreePath, "worktree", "remove", "--force", worktreePath)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to remove worktree: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

func (l LocalBackend) gitCommonDirFromWorktree(worktreePath string) (string, error) {
	cmd := execCommand("git", "-C", worktreePath, "rev-parse", "--git-common-dir")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to determine git common dir: %s", strings.TrimSpace(string(output)))
	}
	commonDir := strings.TrimSpace(string(output))
	if commonDir == "" {
		return "", errors.New("failed to determine git common dir")
	}
	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Join(worktreePath, commonDir)
	}
	return commonDir, nil
}

func (l LocalBackend) deleteGitBranch(commonGitDir string, sessionID string) error {
	if sessionID == "" {
		return nil
	}
	check := execCommand("git", "--git-dir", commonGitDir, "show-ref", "--verify", "--quiet", "refs/heads/"+sessionID)
	if err := check.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return nil
		}
		return fmt.Errorf("failed to check branch: %w", err)
	}
	cmd := execCommand("git", "--git-dir", commonGitDir, "branch", "-D", sessionID)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to delete branch: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

func (l LocalBackend) createTmuxBaseSession(sessionName string, worktreePath string) error {
	newSession := execCommand("tmux", "new-session", "-d", "-s", sessionName, "-n", "nvim", "-c", worktreePath, "nvim")
	if output, err := newSession.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create tmux session: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

func (l LocalBackend) createTmuxOpencodeWindow(sessionName string, worktreePath string, agentConfig AgentConfig, opencodeSessionID string) error {
	opencodeURL := fmt.Sprintf("http://%s:%d", agentConfig.Opencode.Hostname, agentConfig.Opencode.Port)
	opencodeArgs := []string{"new-window", "-t", sessionName, "-n", "opencode", "-c", worktreePath, "opencode", "attach", opencodeURL}
	if strings.TrimSpace(opencodeSessionID) != "" {
		opencodeArgs = append(opencodeArgs, "--session", opencodeSessionID)
	}
	opencodeArgs = append(opencodeArgs, "--dir", worktreePath)
	newOpencode := execCommand("tmux", opencodeArgs...)
	if output, err := newOpencode.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create tmux opencode window: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

func opencodeMessageHasContent(message *messages.Message) bool {
	if message == nil || len(message.Parts) == 0 {
		return false
	}
	for _, part := range message.Parts {
		switch part.Type {
		case messages.PartTypeText:
			if strings.TrimSpace(part.Text) != "" {
				return true
			}
		default:
			return true
		}
	}
	return false
}

func opencodeInputHasContent(agentConfig AgentConfig) bool {
	if opencodeMessageHasContent(agentConfig.Message) {
		return true
	}
	return agentConfig.Command != nil && agentConfig.Command.HasContent()
}

func (l LocalBackend) createTmuxTerminalWindow(sessionName string, worktreePath string) error {
	newTerminal := execCommand("tmux", "new-window", "-t", sessionName, "-n", "terminal", "-c", worktreePath)
	if output, err := newTerminal.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create tmux terminal window: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

func (l LocalBackend) hydrateLocalRuntime(ctx context.Context, sessionName string, worktreePath string, agentConfig AgentConfig) (retErr error) {
	defer func() {
		if retErr == nil {
			return
		}
		_ = l.killTmuxSession(sessionName)
	}()

	if err := l.createTmuxBaseSession(sessionName, worktreePath); err != nil {
		return err
	}

	opencodeConfig := agentConfig.Opencode
	if err := l.ensureOpencodeServer(ctx, worktreePath, opencodeConfig); err != nil {
		return err
	}

	opencodeSessionID, err := l.latestOpencodeSessionID(ctx, opencodeConfig, worktreePath)
	if err != nil {
		return err
	}
	shouldAutorun := false
	if opencodeSessionID == "" && opencodeInputHasContent(agentConfig) {
		opencodeSessionID, err = l.createOpencodeSession(ctx, opencodeConfig, worktreePath)
		if err != nil {
			return err
		}
		shouldAutorun = true
	}

	agentConfig.Opencode = opencodeConfig
	if err := l.createTmuxOpencodeWindow(sessionName, worktreePath, agentConfig, opencodeSessionID); err != nil {
		return err
	}

	if shouldAutorun {
		cfg := opencodeConfig
		session := opencodeSessionID
		dir := worktreePath
		model := agentConfig.Model
		agentName := agentConfig.AgentName
		message := agentConfig.Message
		command := messages.CloneCommand(agentConfig.Command)
		go func() {
			promptCtx, cancel := context.WithTimeout(context.Background(), opencodeAutorunTimeout)
			defer cancel()
			if err := l.sendOpencodeInitialInput(promptCtx, cfg, session, dir, model, agentName, message, command); err != nil {
				slog.Warn(
					"failed to autorun opencode prompt during hydration",
					slog.String("sessionName", sessionName),
					slog.String("opencodeSessionID", session),
					slog.String("error", err.Error()),
				)
			}
		}()
	}

	if err := l.createTmuxTerminalWindow(sessionName, worktreePath); err != nil {
		return err
	}

	return nil
}

func (l LocalBackend) tmuxSessionExists(sessionName string) (bool, error) {
	check := execCommand("tmux", "has-session", "-t", sessionName)
	if err := check.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (l LocalBackend) killTmuxSession(sessionName string) error {
	check := execCommand("tmux", "has-session", "-t", sessionName)
	if err := check.Run(); err != nil {
		return nil
	}
	cmd := execCommand("tmux", "kill-session", "-t", sessionName)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to kill tmux session: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

func (l LocalBackend) ensureOpencodeServer(ctx context.Context, worktreePath string, config conf.OpenCodeConfig) error {
	if l.opencodeHealthy(ctx, config) {
		return nil
	}

	cmd := execCommand(
		"opencode",
		"serve",
		"--hostname",
		config.Hostname,
		"--port",
		fmt.Sprintf("%d", config.Port),
	)

	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		cmd.Dir = home
	} else {
		cmd.Dir = worktreePath
	}

	cmd.Stdin = nil
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	cmd.Env = os.Environ()

	// Detach from any controlling terminal/session (e.g. tmux) so the server
	// keeps running even if the tmux session is killed.
	detachCmd(cmd)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start opencode server: %w", err)
	}
	_ = cmd.Process.Release()

	return l.waitForOpencode(ctx, config, timeouts.SecondLong)
}

func (l LocalBackend) opencodeHealthy(ctx context.Context, config conf.OpenCodeConfig) bool {
	client := &http.Client{Timeout: timeouts.SecondShort}
	url := fmt.Sprintf("http://%s:%d/global/health", config.Hostname, config.Port)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func (l LocalBackend) waitForOpencode(ctx context.Context, config conf.OpenCodeConfig, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return err
		}
		if l.opencodeHealthy(ctx, config) {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for opencode server at %s:%d", config.Hostname, config.Port)
}

func opencodePartsFromMessage(message *messages.Message, worktreePath string) ([]opencode.SessionPromptParamsPartUnion, error) {
	if message == nil || len(message.Parts) == 0 {
		return nil, nil
	}
	parts := make([]opencode.SessionPromptParamsPartUnion, 0, len(message.Parts))
	for _, p := range message.Parts {
		switch p.Type {
		case messages.PartTypeText:
			if strings.TrimSpace(p.Text) == "" {
				continue
			}
			parts = append(parts, opencode.TextPartInputParam{
				Type: opencode.F(opencode.TextPartInputTypeText),
				Text: opencode.F(p.Text),
			})
		case messages.PartTypeFile:
			filePart, err := opencodeFilePartFromMessagePart(p, worktreePath)
			if err != nil {
				return nil, err
			}
			parts = append(parts, filePart)
		}
	}
	return parts, nil
}

func opencodeFilePartFromMessagePart(part messages.MessagePart, worktreePath string) (opencode.FilePartInputParam, error) {
	if part.File == nil {
		return opencode.FilePartInputParam{}, errors.New("file message part is missing file payload")
	}
	filename := strings.TrimSpace(part.File.Filename)
	mimeType := strings.TrimSpace(part.File.Mime)
	inlineURL := ""
	if part.File.URL != nil {
		inlineURL = strings.TrimSpace(*part.File.URL)
	}
	if inlineURL != "" {
		if mimeType == "" {
			return opencode.FilePartInputParam{}, errors.New("inline file message part is missing mime type")
		}
		if filename == "" {
			return opencode.FilePartInputParam{}, errors.New("inline file message part is missing filename")
		}
		return opencode.FilePartInputParam{
			Type:     opencode.F(opencode.FilePartInputTypeFile),
			URL:      opencode.F(inlineURL),
			Mime:     opencode.F(mimeType),
			Filename: opencode.F(filename),
		}, nil
	}
	if part.File.Source == nil {
		return opencode.FilePartInputParam{}, errors.New("file message part is missing file source")
	}
	if strings.TrimSpace(worktreePath) == "" {
		return opencode.FilePartInputParam{}, errors.New("worktree path is required for file message parts")
	}
	relativePath := part.File.Source.Path
	absolutePath := filepath.Join(worktreePath, relativePath)
	info, err := os.Stat(absolutePath)
	if err != nil {
		return opencode.FilePartInputParam{}, fmt.Errorf("resolve file part %q: %w", relativePath, err)
	}
	if info.IsDir() {
		return opencode.FilePartInputParam{}, fmt.Errorf("resolve file part %q: path is a directory", relativePath)
	}
	fileURL, err := localFileURL(absolutePath)
	if err != nil {
		return opencode.FilePartInputParam{}, fmt.Errorf("resolve file part %q url: %w", relativePath, err)
	}
	if filename == "" {
		filename = filepath.Base(relativePath)
	}
	if mimeType == "" {
		mimeType = mimeTypeForPath(relativePath)
	}
	source := opencode.FilePartSourceUnionParam(opencode.FileSourceParam{
		Type: opencode.F(opencode.FileSourceTypeFile),
		Path: opencode.F(relativePath),
		Text: opencode.F(opencode.FilePartSourceTextParam{}),
	})
	if part.File.Source != nil && part.File.Source.Text != nil {
		source = opencode.FileSourceParam{
			Type: opencode.F(opencode.FileSourceTypeFile),
			Path: opencode.F(relativePath),
			Text: opencode.F(opencode.FilePartSourceTextParam{
				Start: opencode.F(part.File.Source.Text.Start),
				End:   opencode.F(part.File.Source.Text.End),
				Value: opencode.F(part.File.Source.Text.Value),
			}),
		}
	}
	return opencode.FilePartInputParam{
		Type:     opencode.F(opencode.FilePartInputTypeFile),
		URL:      opencode.F(fileURL),
		Mime:     opencode.F(mimeType),
		Filename: opencode.F(filename),
		Source:   opencode.F(source),
	}, nil
}

func mimeTypeForPath(path string) string {
	if detected := mime.TypeByExtension(filepath.Ext(path)); detected != "" {
		return detected
	}
	return "text/plain"
}

func localFileURL(path string) (string, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(absPath)}).String(), nil
}

func (l LocalBackend) createOpencodeSession(ctx context.Context, config conf.OpenCodeConfig, worktreePath string) (string, error) {
	return newOpencodeClient(config).CreateSession(ctx, worktreePath)
}

func (l LocalBackend) latestOpencodeSessionID(ctx context.Context, config conf.OpenCodeConfig, worktreePath string) (string, error) {
	return newOpencodeClient(config).LatestSessionID(ctx, worktreePath)
}

func (l LocalBackend) seedOpencodeMessage(ctx context.Context, config conf.OpenCodeConfig, sessionID string, directory string, model string, agentName string, message *messages.Message) error {
	return newOpencodeClient(config).SendPrompt(ctx, sessionID, directory, model, agentName, message, true)
}

func (l LocalBackend) sendOpencodeMessage(ctx context.Context, config conf.OpenCodeConfig, sessionID string, directory string, model string, agentName string, message *messages.Message) error {
	return newOpencodeClient(config).SendPrompt(ctx, sessionID, directory, model, agentName, message, false)
}

func (l LocalBackend) sendOpencodeCommand(ctx context.Context, config conf.OpenCodeConfig, sessionID string, directory string, model string, agentName string, command *messages.CommandInvocation) error {
	return newOpencodeClient(config).SendCommand(ctx, sessionID, directory, model, agentName, command)
}

func (l LocalBackend) sendOpencodeInitialInput(ctx context.Context, config conf.OpenCodeConfig, sessionID string, directory string, model string, agentName string, message *messages.Message, command *messages.CommandInvocation) error {
	if command != nil && command.HasContent() {
		return l.sendOpencodeCommand(ctx, config, sessionID, directory, model, agentName, command)
	}
	return l.sendOpencodeMessage(ctx, config, sessionID, directory, model, agentName, message)
}

func parseOpencodeModel(raw string) (providerID string, modelID string, ok bool) {
	parts := strings.SplitN(strings.TrimSpace(raw), "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func (l LocalBackend) moveGitWorktree(repoPath string, fromPath string, toPath string) error {
	cmd := execCommand("git", "-C", repoPath, "worktree", "move", fromPath, toPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to move worktree: %s: %s", err.Error(), strings.TrimSpace(string(output)))
	}
	return nil
}

func (l LocalBackend) resetAndCleanWorktree(worktreePath string) error {
	reset := execCommand("git", "-C", worktreePath, "reset", "--hard")
	if output, err := reset.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to reset worktree: %s: %s", err.Error(), strings.TrimSpace(string(output)))
	}
	clean := execCommand("git", "-C", worktreePath, "clean", "-ffd")
	if output, err := clean.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to clean worktree: %s: %s", err.Error(), strings.TrimSpace(string(output)))
	}
	return nil
}

func (l LocalBackend) checkoutNewBranch(worktreePath string, branchName string, baseRef string) error {
	cmd := execCommand("git", "-C", worktreePath, "checkout", "-B", branchName, baseRef)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to checkout branch: %s: %s", err.Error(), strings.TrimSpace(string(output)))
	}
	return nil
}

func (l LocalBackend) resolveBaseRef(repoPath string) (string, error) {
	symbolic := execCommand("git", "-C", repoPath, "symbolic-ref", "refs/remotes/origin/HEAD")
	if output, err := symbolic.CombinedOutput(); err == nil {
		ref := strings.TrimSpace(string(output))
		if ref != "" {
			return ref, nil
		}
	}
	for _, ref := range []string{
		"refs/remotes/origin/main",
		"refs/remotes/origin/master",
		"refs/heads/main",
		"refs/heads/master",
	} {
		check := execCommand("git", "-C", repoPath, "show-ref", "--verify", "--quiet", ref)
		if err := check.Run(); err == nil {
			return ref, nil
		}
	}
	return "HEAD", nil
}
