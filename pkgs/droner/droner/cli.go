package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Oudwins/droner/pkgs/droner/internals/desktop"
	"github.com/Oudwins/droner/pkgs/droner/internals/schemas"
	"github.com/Oudwins/droner/pkgs/droner/internals/term"
	"github.com/Oudwins/droner/pkgs/droner/sdk"

	z "github.com/Oudwins/zog"
)

var ErrUsage = errors.New("usage:\n  droner new [--path <path>] [--id <id>] [--model <model>] [--prompt <prompt>] [--wait] [--wait-timeout <duration>]\n  droner del <id> [--wait] [--wait-timeout <duration>]\n  droner task <id>\n  droner auth github")

type NewArgs struct {
	Path    string `zog:"path"`
	ID      string `zog:"id"`
	Model   string `zog:"model"`
	Prompt  string `zog:"prompt"`
	Wait    bool   `zog:"wait"`
	Timeout string `zog:"timeout"`
}

type DelArgs struct {
	ID      string `zog:"id"`
	Wait    bool   `zog:"wait"`
	Timeout string `zog:"timeout"`
}

var newArgsSchema = z.Struct(z.Shape{
	"Path":    z.String().Optional().Trim(),
	"ID":      z.String().Optional().Trim(),
	"Model":   z.String().Optional().Trim(),
	"Prompt":  z.String().Optional().Trim(),
	"Wait":    z.Bool().Optional(),
	"Timeout": z.String().Optional().Trim(),
})

var delArgsSchema = z.Struct(z.Shape{
	"ID":      z.String().Required().Trim(),
	"Wait":    z.Bool().Optional(),
	"Timeout": z.String().Optional().Trim(),
})

func main() {
	if err := run(os.Args[1:]); err != nil {
		if errors.Is(err, ErrUsage) {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return ErrUsage
	}

	command := args[0]
	client := sdk.NewClient()

	switch command {
	case "new":
		parsed, err := parseNewArgs(args[1:])
		if err != nil {
			return err
		}
		if parsed.Path == "" {
			repoRoot, err := repoRootFromCwd()
			if err != nil {
				return err
			}
			parsed.Path = repoRoot
		}
		if err := validateNewArgs(&parsed); err != nil {
			return err
		}
		if err := ensureDaemonRunning(client); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		request := schemas.SessionCreateRequest{Path: parsed.Path, SessionID: parsed.ID}
		if parsed.Model != "" || parsed.Prompt != "" {
			request.Agent = &schemas.SessionAgentConfig{Model: parsed.Model, Prompt: parsed.Prompt}
		}
		response, err := client.CreateSession(ctx, request)
		if err != nil {
			if errors.Is(err, sdk.ErrAuthRequired) {
				if err := runGitHubAuthFlow(client); err != nil {
					return err
				}
				ctx, retryCancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer retryCancel()
				response, err = client.CreateSession(ctx, request)
				if err != nil {
					return err
				}
			} else {
				return err
			}
		}
		printTaskSummary(response)
		if parsed.Wait {
			timeout, err := parseWaitTimeout(parsed.Timeout)
			if err != nil {
				return err
			}
			final, err := waitForTask(client, response.TaskID, timeout)
			if err != nil {
				return err
			}
			printTaskSummary(final)
		}
		return nil
	case "del":
		parsed, err := parseDelArgs(args[1:])
		if err != nil {
			return err
		}
		if err := validateDelArgs(&parsed); err != nil {
			return err
		}
		if err := ensureDaemonRunning(client); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		response, err := client.DeleteSession(ctx, schemas.SessionDeleteRequest{SessionID: parsed.ID})
		if err != nil {
			return err
		}
		printTaskSummary(response)
		if parsed.Wait {
			timeout, err := parseWaitTimeout(parsed.Timeout)
			if err != nil {
				return err
			}
			final, err := waitForTask(client, response.TaskID, timeout)
			if err != nil {
				return err
			}
			printTaskSummary(final)
		}
		return nil
	case "task":
		if len(args) != 2 {
			return ErrUsage
		}
		if err := ensureDaemonRunning(client); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		response, err := client.TaskStatus(ctx, args[1])
		if err != nil {
			return err
		}
		printTaskSummary(response)
		return nil
	case "auth":
		if len(args) != 2 || args[1] != "github" {
			return ErrUsage
		}
		if err := ensureDaemonRunning(client); err != nil {
			return err
		}
		return runGitHubAuthFlow(client)
	default:
		return ErrUsage
	}
}

func parseNewArgs(args []string) (NewArgs, error) {
	parsed := NewArgs{}
	for i := 0; i < len(args); {
		switch args[i] {
		case "--path":
			if i+1 >= len(args) {
				return parsed, ErrUsage
			}
			parsed.Path = args[i+1]
			i += 2
		case "--id":
			if i+1 >= len(args) {
				return parsed, ErrUsage
			}
			parsed.ID = args[i+1]
			i += 2
		case "--model":
			if i+1 >= len(args) {
				return parsed, ErrUsage
			}
			parsed.Model = args[i+1]
			i += 2
		case "--prompt":
			if i+1 >= len(args) {
				return parsed, ErrUsage
			}
			parsed.Prompt = args[i+1]
			i += 2
		case "--wait":
			parsed.Wait = true
			i += 1
		case "--wait-timeout":
			if i+1 >= len(args) {
				return parsed, ErrUsage
			}
			parsed.Timeout = args[i+1]
			i += 2
		default:
			return parsed, ErrUsage
		}
	}
	return parsed, nil
}

func parseDelArgs(args []string) (DelArgs, error) {
	if len(args) < 1 {
		return DelArgs{}, ErrUsage
	}
	parsed := DelArgs{ID: args[0]}
	for i := 1; i < len(args); {
		switch args[i] {
		case "--wait":
			parsed.Wait = true
			i += 1
		case "--wait-timeout":
			if i+1 >= len(args) {
				return parsed, ErrUsage
			}
			parsed.Timeout = args[i+1]
			i += 2
		default:
			return parsed, ErrUsage
		}
	}
	return parsed, nil
}

func validateNewArgs(payload *NewArgs) error {
	if issues := newArgsSchema.Validate(payload); len(issues) > 0 {
		return fmt.Errorf("invalid arguments:\n%s", z.Issues.Prettify(issues))
	}
	return nil
}

func validateDelArgs(payload *DelArgs) error {
	if issues := delArgsSchema.Validate(payload); len(issues) > 0 {
		return fmt.Errorf("invalid arguments:\n%s", z.Issues.Prettify(issues))
	}
	return nil
}

func ensureDaemonRunning(client *sdk.Client) error {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	if _, err := client.Version(ctx); err == nil {
		return nil
	}

	if err := startDaemon(); err != nil {
		return err
	}

	return waitForDaemon(client)
}

func runGitHubAuthFlow(client *sdk.Client) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start, err := client.StartGitHubOAuth(ctx)
	if err != nil {
		return err
	}

	url := start.VerificationURIComplete
	if url == "" {
		url = start.VerificationURI
	}

	if url != "" {
		_ = desktop.OpenURL(url)
		fmt.Printf("Authenticate with GitHub to continue:\nURL: %s\n", term.ClickableLink(url, url))
	} else {
		fmt.Println("Authenticate with GitHub to continue:")
	}
	if start.UserCode != "" {
		fmt.Printf("User code: %s\n", start.UserCode)
	}

	deadline := time.Now().Add(2 * time.Minute)
	if start.ExpiresIn > 0 {
		expiry := time.Now().Add(time.Duration(start.ExpiresIn) * time.Second)
		if expiry.Before(deadline) {
			deadline = expiry
		}
	}
	pollInterval := 2 * time.Second
	if start.Interval > 0 {
		pollInterval = time.Duration(start.Interval) * time.Second
		if pollInterval < 2*time.Second {
			pollInterval = 2 * time.Second
		}
	}
	for time.Now().Before(deadline) {
		pollCtx, pollCancel := context.WithTimeout(context.Background(), 3*time.Second)
		status, err := client.GitHubOAuthStatus(pollCtx, start.State)
		pollCancel()
		if err != nil {
			return err
		}
		switch status.Status {
		case "complete":
			fmt.Println("GitHub auth complete.")
			return nil
		case "failed":
			if status.Error != "" {
				return fmt.Errorf("github auth failed: %s", status.Error)
			}
			return errors.New("github auth failed")
		default:
			time.Sleep(pollInterval)
		}
	}

	return errors.New("timed out waiting for github auth")
}

func startDaemon() error {
	path, err := findDaemonBinary()
	if err != nil {
		return err
	}

	cmd := exec.Command(path)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Start()
}

func waitForDaemon(client *sdk.Client) error {
	var lastErr error
	for i := 0; i < 8; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		_, err := client.Version(ctx)
		cancel()
		if err == nil {
			return nil
		}
		lastErr = err
		time.Sleep(time.Duration(i+1) * 150 * time.Millisecond)
	}

	if lastErr != nil {
		return lastErr
	}
	return errors.New("failed to reach dronerd")
}

func parseWaitTimeout(raw string) (time.Duration, error) {
	if strings.TrimSpace(raw) == "" {
		return 45 * time.Minute, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid wait timeout: %w", err)
	}
	return value, nil
}

func waitForTask(client *sdk.Client, taskID string, timeout time.Duration) (*schemas.TaskResponse, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		response, err := client.TaskStatus(ctx, taskID)
		cancel()
		if err != nil {
			return nil, err
		}
		switch response.Status {
		case schemas.TaskStatusSucceeded:
			return response, nil
		case schemas.TaskStatusFailed:
			if response.Error != "" {
				return response, fmt.Errorf("task failed: %s", response.Error)
			}
			return response, errors.New("task failed")
		default:
			time.Sleep(2 * time.Second)
		}
	}

	return nil, fmt.Errorf("timed out waiting for task %s", taskID)
}

func printTaskSummary(response *schemas.TaskResponse) {
	fmt.Printf("task: %s\nstatus: %s\n", response.TaskID, response.Status)
	if response.Result != nil {
		if response.Result.SessionID != "" {
			fmt.Printf("session: %s\n", response.Result.SessionID)
		}
		if response.Result.WorktreePath != "" {
			fmt.Printf("worktree: %s\n", response.Result.WorktreePath)
		}
	}
	if response.Error != "" {
		fmt.Printf("error: %s\n", response.Error)
	}
}

func findDaemonBinary() (string, error) {
	executable, err := os.Executable()
	if err == nil {
		candidate := filepath.Join(filepath.Dir(executable), "dronerd")
		if _, statErr := os.Stat(candidate); statErr == nil {
			return candidate, nil
		}
	}

	path, err := exec.LookPath("dronerd")
	if err != nil {
		return "", fmt.Errorf("dronerd not found in PATH")
	}
	return path, nil
}

func repoRootFromCwd() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to determine repo root: %s", strings.TrimSpace(string(output)))
	}
	root := strings.TrimSpace(string(output))
	if root == "" {
		return "", errors.New("failed to determine repo root")
	}
	return root, nil
}
