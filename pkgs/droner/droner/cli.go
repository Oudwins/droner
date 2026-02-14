package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/Oudwins/droner/pkgs/droner/dronerd/server"
	"github.com/Oudwins/droner/pkgs/droner/internals/cliutil"
	"github.com/Oudwins/droner/pkgs/droner/internals/messages"
	"github.com/Oudwins/droner/pkgs/droner/internals/schemas"
	"github.com/Oudwins/droner/pkgs/droner/sdk"
	"github.com/Oudwins/droner/pkgs/droner/tui"
	"github.com/mattn/go-isatty"
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

var newArgsSchema = z.Struct(z.Shape{
	"Path":    z.String().Optional().Trim(),
	"ID":      z.String().Optional().Trim(),
	"Model":   z.String().Optional().Trim(),
	"Prompt":  z.String().Optional().Trim(),
	"Wait":    z.Bool().Optional(),
	"Timeout": z.String().Optional().Trim(),
})

type ServeArgs struct {
	Detach bool
}

type DelArgs struct {
	ID      string `zog:"id"`
	Wait    bool   `zog:"wait"`
	Timeout string `zog:"timeout"`
}

type NukeArgs struct {
	Yes     bool
	Wait    bool
	Timeout string
}

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
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return cmd.Usage()
			}
			if !isInteractiveTerminal() {
				return cmd.Usage()
			}
			client := sdk.NewClient()
			return tui.Run(client)
		},
	}

	cmd.AddCommand(
		newServeCmd(),
		newNewCmd(),
		newDelCmd(),
		newNukeCmd(),
		newSessionsCmd(),
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
				return cliutil.StartDaemon()
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
			includeAgentConfig := cmd.Flags().Changed("model") || cmd.Flags().Changed("prompt") || args.Model != "" || args.Prompt != ""

			return runCreateSession(&args, includeAgentConfig)
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
			if err := cliutil.EnsureDaemonRunning(client); err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			response, err := client.DeleteSession(ctx, schemas.SessionDeleteRequest{SessionID: schemas.NewSSessionID(args.ID)})
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

func newNukeCmd() *cobra.Command {
	args := NukeArgs{}
	cmd := &cobra.Command{
		Use:   "nuke",
		Short: "Delete all sessions",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client := sdk.NewClient()
			if err := cliutil.EnsureDaemonRunning(client); err != nil {
				return err
			}
			if !args.Yes {
				if !isInteractiveTerminal() {
					return errors.New("refusing to delete all sessions without --yes in non-interactive mode")
				}
				confirmed, err := confirmAction("This will delete all sessions. Continue? [y/N]: ")
				if err != nil {
					return err
				}
				if !confirmed {
					fmt.Println("Aborted.")
					return nil
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			response, err := client.NukeSessions(ctx)
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

	cmd.Flags().BoolVar(&args.Yes, "dangerously-skip-confirmation", false, "skip confirmation prompt")
	cmd.Flags().BoolVar(&args.Wait, "wait", false, "wait for the task to complete")
	cmd.Flags().StringVar(&args.Timeout, "wait-timeout", "", "maximum wait duration")
	return cmd
}

func newSessionsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sessions",
		Short: "List queued and running sessions",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client := sdk.NewClient()
			if err := cliutil.EnsureDaemonRunning(client); err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			response, err := client.ListSessions(ctx)
			if err != nil {
				if errors.Is(err, sdk.ErrAuthRequired) {
					if err := cliutil.RunGitHubAuthFlow(client); err != nil {
						return err
					}
					ctx, retryCancel := context.WithTimeout(context.Background(), 3*time.Second)
					defer retryCancel()
					response, err = client.ListSessions(ctx)
					if err != nil {
						return err
					}
				} else {
					return err
				}
			}
			if len(response.Sessions) == 0 {
				fmt.Println("No queued or running sessions.")
				return nil
			}
			writer := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(writer, "simpleId\tstate")
			for _, session := range response.Sessions {
				fmt.Fprintf(writer, "%s\t%s\n", session.SimpleID, session.State)
			}
			writer.Flush()
			return nil
		},
	}

	return cmd
}

func newTaskCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "task <id>",
		Short: "Check a task status",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, inputs []string) error {
			client := sdk.NewClient()
			if err := cliutil.EnsureDaemonRunning(client); err != nil {
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
			if err := cliutil.EnsureDaemonRunning(client); err != nil {
				return err
			}
			return cliutil.RunGitHubAuthFlow(client)
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

func runCreateSession(args *NewArgs, includeAgentConfig bool) error {
	client := sdk.NewClient()
	if args.Path == "" {
		repoRoot, err := cliutil.RepoRootFromCwd()
		if err != nil {
			return err
		}
		args.Path = repoRoot
	}
	if err := validateNewArgs(args); err != nil {
		return err
	}
	if err := cliutil.EnsureDaemonRunning(client); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	request := schemas.SessionCreateRequest{Path: args.Path, SessionID: schemas.NewSSessionID(args.ID)}
	if includeAgentConfig {
		agentConfig := &schemas.SessionAgentConfig{Model: args.Model}
		prompt := strings.TrimSpace(args.Prompt)
		if prompt != "" {
			agentConfig.Message = &messages.Message{Parts: []messages.MessagePart{{Type: messages.PartTypeText, Text: prompt}}}
		}
		request.AgentConfig = agentConfig
	}
	response, err := client.CreateSession(ctx, request)
	if err != nil {
		if errors.Is(err, sdk.ErrAuthRequired) {
			if err := cliutil.RunGitHubAuthFlow(client); err != nil {
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
	cliutil.PrintSessionCreated(response)
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
}

func isInteractiveTerminal() bool {
	stdin := os.Stdin.Fd()
	stdout := os.Stdout.Fd()
	return (isatty.IsTerminal(stdin) || isatty.IsCygwinTerminal(stdin)) &&
		(isatty.IsTerminal(stdout) || isatty.IsCygwinTerminal(stdout))
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

func confirmAction(prompt string) (bool, error) {
	reader := bufio.NewReader(os.Stdin)
	fmt.Fprint(os.Stdout, prompt)
	input, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	input = strings.TrimSpace(strings.ToLower(input))
	return input == "y" || input == "yes", nil
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
		if sessionID := taskSimpleSessionID(response.Result); sessionID != "" {
			fmt.Printf("session: %s\n", sessionID)
		}
		if response.Result.WorktreePath != "" {
			fmt.Printf("worktree: %s\n", response.Result.WorktreePath)
		}
	}
	if response.Error != "" {
		fmt.Printf("error: %s\n", response.Error)
	}
}

func taskSimpleSessionID(result *schemas.TaskResult) string {
	if result == nil {
		return ""
	}
	if result.SessionID != "" && !looksLikeUUID(result.SessionID) {
		return result.SessionID
	}
	if result.WorktreePath != "" {
		base := filepath.Base(result.WorktreePath)
		if parts := strings.SplitN(base, schemas.SimpleSessionDelimiter, 2); len(parts) == 2 && parts[1] != "" {
			return parts[1]
		}
	}
	return result.SessionID
}

func looksLikeUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	return value[8] == '-' && value[13] == '-' && value[18] == '-' && value[23] == '-'
}
