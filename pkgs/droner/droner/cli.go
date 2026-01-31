package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Oudwins/droner/pkgs/droner/dronerd/server"
	"github.com/Oudwins/droner/pkgs/droner/internals/conf"
	"github.com/Oudwins/droner/pkgs/droner/internals/desktop"
	"github.com/Oudwins/droner/pkgs/droner/internals/schemas"
	"github.com/Oudwins/droner/pkgs/droner/internals/term"
	"github.com/Oudwins/droner/pkgs/droner/sdk"
	"github.com/spf13/cobra"

	z "github.com/Oudwins/zog"
)

type NewArgs struct {
	Path    string `zog:"path"`
	ID      string `zog:"id"`
	Model   string `zog:"model"`
	Prompt  string `zog:"prompt"`
	Wait    bool   `zog:"wait"`
	Timeout string `zog:"timeout"`
}

type ServeArgs struct {
	Detach bool
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
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "droner",
		Short: "Droner CLI",
	}

	cmd.AddCommand(
		newServeCmd(),
		newNewCmd(),
		newDelCmd(),
		newTaskCmd(),
		newAuthCmd(),
	)

	return cmd
}

func newServeCmd() *cobra.Command {
	args := ServeArgs{}
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the droner server",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if args.Detach {
				return startDaemon()
			}
			serverInstance := server.New()
			if err := serverInstance.Start(); err != nil {
				return fmt.Errorf("failed to start server: %w", err)
			}
			return nil
		},
	}

	cmd.Flags().BoolVarP(&args.Detach, "detach", "d", false, "run server in background")
	return cmd
}

func newNewCmd() *cobra.Command {
	args := NewArgs{}
	cmd := &cobra.Command{
		Use:   "new",
		Short: "Create a new session",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client := sdk.NewClient()
			if args.Path == "" {
				repoRoot, err := repoRootFromCwd()
				if err != nil {
					return err
				}
				args.Path = repoRoot
			}
			if err := validateNewArgs(&args); err != nil {
				return err
			}
			if err := ensureDaemonRunning(client); err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			request := schemas.SessionCreateRequest{Path: args.Path, SessionID: args.ID}
			if args.Model != "" || args.Prompt != "" {
				request.Agent = &schemas.SessionAgentConfig{Model: args.Model, Prompt: args.Prompt}
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
			if args.Wait {
				timeout, err := parseWaitTimeout(args.Timeout)
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
		},
	}

	cmd.Flags().StringVar(&args.Path, "path", "", "path to the repository")
	cmd.Flags().StringVar(&args.ID, "id", "", "session ID")
	cmd.Flags().StringVar(&args.Model, "model", "", "agent model")
	cmd.Flags().StringVar(&args.Prompt, "prompt", "", "agent prompt")
	cmd.Flags().BoolVar(&args.Wait, "wait", false, "wait for the task to complete")
	cmd.Flags().StringVar(&args.Timeout, "wait-timeout", "", "maximum wait duration")
	return cmd
}

func newDelCmd() *cobra.Command {
	args := DelArgs{}
	cmd := &cobra.Command{
		Use:   "del <id>",
		Short: "Delete a session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, inputs []string) error {
			client := sdk.NewClient()
			args.ID = inputs[0]
			if err := validateDelArgs(&args); err != nil {
				return err
			}
			if err := ensureDaemonRunning(client); err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			response, err := client.DeleteSession(ctx, schemas.SessionDeleteRequest{SessionID: args.ID})
			if err != nil {
				return err
			}
			printTaskSummary(response)
			if args.Wait {
				timeout, err := parseWaitTimeout(args.Timeout)
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
		},
	}

	cmd.Flags().BoolVar(&args.Wait, "wait", false, "wait for the task to complete")
	cmd.Flags().StringVar(&args.Timeout, "wait-timeout", "", "maximum wait duration")
	return cmd
}

func newTaskCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "task <id>",
		Short: "Check a task status",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, inputs []string) error {
			client := sdk.NewClient()
			if err := ensureDaemonRunning(client); err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			response, err := client.TaskStatus(ctx, inputs[0])
			if err != nil {
				return err
			}
			printTaskSummary(response)
			return nil
		},
	}

	return cmd
}

func newAuthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth <provider>",
		Short: "Authenticate with a provider",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, inputs []string) error {
			provider := inputs[0]
			if provider != "github" {
				return fmt.Errorf("unsupported auth provider: %s", provider)
			}
			client := sdk.NewClient()
			if err := ensureDaemonRunning(client); err != nil {
				return err
			}
			return runGitHubAuthFlow(client)
		},
	}

	return cmd
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

	if version, err := client.Version(ctx); err == nil {
		localVersion := conf.GetConfig().Version
		if strings.TrimSpace(version) == strings.TrimSpace(localVersion) {
			return nil
		}
		return replaceDaemon(client, version, localVersion)
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
	path, err := findServeBinary()
	if err != nil {
		return err
	}

	cmd := exec.Command(path, "serve")
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
	return errors.New("failed to reach droner server")
}

func replaceDaemon(client *sdk.Client, remoteVersion string, localVersion string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := client.Shutdown(ctx); err != nil {
		if errors.Is(err, sdk.ErrShutdownUnsupported) {
			return fmt.Errorf("dronerd %s is running; please stop it and retry", strings.TrimSpace(remoteVersion))
		}
		return fmt.Errorf("failed to shutdown dronerd %s: %w", strings.TrimSpace(remoteVersion), err)
	}

	if err := waitForDaemonStop(client); err != nil {
		return fmt.Errorf("dronerd %s did not stop: %w", strings.TrimSpace(remoteVersion), err)
	}

	if err := startDaemon(); err != nil {
		return err
	}

	return waitForDaemon(client)
}

func waitForDaemonStop(client *sdk.Client) error {
	for i := 0; i < 8; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		_, err := client.Version(ctx)
		cancel()
		if err != nil {
			return nil
		}
		time.Sleep(time.Duration(i+1) * 150 * time.Millisecond)
	}

	return errors.New("failed to stop dronerd")
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

func findServeBinary() (string, error) {
	executable, err := os.Executable()
	if err == nil && executable != "" {
		return executable, nil
	}

	path, err := exec.LookPath("droner")
	if err != nil {
		return "", fmt.Errorf("droner not found in PATH")
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
