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

	"github.com/Oudwins/droner/pkgs/droner/dronerd/server"
	"github.com/Oudwins/droner/pkgs/droner/internals/cliutil"
	"github.com/Oudwins/droner/pkgs/droner/internals/messages"
	"github.com/Oudwins/droner/pkgs/droner/internals/schemas"
	"github.com/Oudwins/droner/pkgs/droner/internals/timeouts"
	"github.com/Oudwins/droner/pkgs/droner/internals/version"
	"github.com/Oudwins/droner/pkgs/droner/sdk"
	"github.com/Oudwins/droner/pkgs/droner/tui"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	z "github.com/Oudwins/zog"
)

type NewArgs struct {
	Path      string `zog:"path"`
	Branch    string `zog:"branch"`
	Model     string `zog:"model"`
	AgentName string `zog:"agent"`
	Prompt    string `zog:"prompt"`
}

var newArgsSchema = z.Struct(z.Shape{
	"Path":      z.String().Optional().Trim(),
	"Branch":    z.String().Optional().Trim(),
	"Model":     z.String().Optional().Trim(),
	"AgentName": z.String().Optional().Trim(),
	"Prompt":    z.String().Optional().Trim(),
})

type ServeArgs struct {
	Detach bool
}

type DelArgs struct {
	ID string `zog:"id"`
}

type CompleteArgs struct {
	ID string `zog:"id"`
}

type NukeArgs struct {
	Yes bool
}

var delArgsSchema = z.Struct(z.Shape{
	"ID": z.String().Required().Trim(),
})

func main() {
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	showVersion := false
	cmd := &cobra.Command{
		Use:   "droner",
		Short: "Droner CLI",
		RunE: func(cmd *cobra.Command, args []string) error {
			if showVersion {
				printVersionInfo(cmd)
				return nil
			}
			if len(args) > 0 {
				return cmd.Usage()
			}
			return runTUICmd(cmd, nil)
		},
	}

	cmd.Flags().BoolVar(&showVersion, "version", false, "print CLI/server versions and exit")

	cmd.AddCommand(
		newServeCmd(),
		newTUICmd(),
		newNewCmd(),
		newDelCmd(),
		newCompleteCmd(),
		newNukeCmd(),
		newSessionsCmd(),
	)

	return cmd
}

func newTUICmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Open the Droner TUI",
		Args:  cobra.NoArgs,
		RunE:  runTUICmd,
	}
}

func runTUICmd(cmd *cobra.Command, _ []string) error {
	if !isInteractiveTerminal() {
		return cmd.Usage()
	}
	client := sdk.NewClient()
	return tui.Run(client)
}

func printVersionInfo(cmd *cobra.Command) {
	out := cmd.OutOrStdout()
	_, _ = fmt.Fprintf(out, "cli: %s\n", strings.TrimSpace(version.Version()))

	ctx, cancel := context.WithTimeout(context.Background(), timeouts.Probe)
	defer cancel()

	serverVersion, err := sdk.NewClient().Version(ctx)
	if err != nil || strings.TrimSpace(serverVersion) == "" {
		_, _ = fmt.Fprintln(out, "server: (not running)")
		return
	}
	_, _ = fmt.Fprintf(out, "server: %s\n", strings.TrimSpace(serverVersion))
}

func newCompleteCmd() *cobra.Command {
	args := CompleteArgs{}
	cmd := &cobra.Command{
		Use:   "complete <id>",
		Short: "Complete a running session (keeps worktree)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, inputs []string) error {
			client := sdk.NewClient()
			args.ID = inputs[0]
			if err := validateDelArgs(&DelArgs{ID: args.ID}); err != nil {
				return err
			}
			if err := cliutil.EnsureDaemonRunning(client); err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(context.Background(), timeouts.SecondShort)
			defer cancel()
			response, err := client.CompleteSession(ctx, schemas.SessionCompleteRequest{Branch: schemas.NewSBranch(args.ID)})
			if err != nil {
				return err
			}
			printOperationSummary(response)
			return nil
		},
	}
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

			includeAgentConfig := cmd.Flags().Changed("model") || cmd.Flags().Changed("agent") || cmd.Flags().Changed("prompt") || args.Model != "" || args.AgentName != "" || args.Prompt != ""

			return runCreateSession(&args, includeAgentConfig)
		},
	}

	cmd.Flags().StringVar(&args.Path, "path", "", "path to the repository")
	cmd.Flags().StringVar(&args.Branch, "branch", "", "branch name")
	cmd.Flags().StringVar(&args.Model, "model", "", "agent model")
	cmd.Flags().StringVar(&args.AgentName, "agent", "", "opencode agent")
	cmd.Flags().StringVar(&args.Prompt, "prompt", "", "agent prompt")
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
			ctx, cancel := context.WithTimeout(context.Background(), timeouts.SecondShort)
			defer cancel()
			response, err := client.DeleteSession(ctx, schemas.SessionDeleteRequest{Branch: schemas.NewSBranch(args.ID)})
			if err != nil {
				return err
			}
			printOperationSummary(response)
			return nil
		},
	}
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
			ctx, cancel := context.WithTimeout(context.Background(), timeouts.SecondShort)
			defer cancel()
			response, err := client.NukeSessions(ctx)
			if err != nil {
				return err
			}
			printOperationSummary(response)
			return nil
		},
	}

	cmd.Flags().BoolVar(&args.Yes, "dangerously-skip-confirmation", false, "skip confirmation prompt")
	return cmd
}

func newSessionsCmd() *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "sessions",
		Short: "List sessions (defaults to running)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client := sdk.NewClient()
			if err := cliutil.EnsureDaemonRunning(client); err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(context.Background(), timeouts.SecondShort)
			defer cancel()
			var response *schemas.SessionListResponse
			var err error
			if all {
				// Explicitly request any status: pass nil statuses and no cursor
				response, err = client.ListSessionsWithParams(ctx, nil, 0, "", "")
			} else {
				// Default to listing running sessions in the CLI
				response, err = client.ListSessionsWithParams(ctx, []string{string(sdk.SessionStatusQueued), string(sdk.SessionStatusRunning)}, 0, "", "")
			}
			if err != nil {
				return err
			}
			if len(response.Sessions) == 0 {
				if all {
					fmt.Println("No sessions.")
				} else {
					fmt.Println("No running sessions.")
				}
				return nil
			}
			writer := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(writer, "id\trepo\tremoteUrl\tbranch\tstate")
			for _, session := range response.Sessions {
				fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\n", session.ID, session.Repo, session.RemoteURL, session.Branch, session.State)
			}
			writer.Flush()
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "list last 100 sessions of any status (no status filter). By default lists running sessions")
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
	ctx, cancel := context.WithTimeout(context.Background(), timeouts.SecondLong)
	defer cancel()
	request := schemas.SessionCreateRequest{Path: args.Path, Branch: schemas.NewSBranch(args.Branch)}
	if includeAgentConfig {
		agentConfig := &schemas.SessionAgentConfig{Model: args.Model, AgentName: strings.TrimSpace(args.AgentName)}
		prompt := strings.TrimSpace(args.Prompt)
		if prompt != "" {
			agentConfig.Message = &messages.Message{Parts: []messages.MessagePart{{Type: messages.PartTypeText, Text: prompt}}}
		}
		request.AgentConfig = agentConfig
	}
	response, err := client.CreateSession(ctx, request)
	if err != nil {
		return err
	}
	cliutil.PrintSessionCreated(response)
	return nil
}

func isInteractiveTerminal() bool {
	stdin := os.Stdin.Fd()
	stdout := os.Stdout.Fd()
	return (isatty.IsTerminal(stdin) || isatty.IsCygwinTerminal(stdin)) &&
		(isatty.IsTerminal(stdout) || isatty.IsCygwinTerminal(stdout))
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

func printOperationSummary(response *schemas.TaskResponse) {
	if response.Type == "session_nuke" {
		fmt.Printf("status: %s\n", response.Status)
		if response.Result != nil && response.Result.Requested != "" {
			fmt.Printf("requested: %s\n", response.Result.Requested)
		}
		if response.Error != "" {
			fmt.Printf("error: %s\n", response.Error)
		}
		return
	}

	fmt.Printf("status: %s\n", response.Status)
	if response.Result != nil {
		if branch := operationBranch(response.Result); branch != "" {
			fmt.Printf("branch: %s\n", branch)
		}
		if response.Result.WorktreePath != "" {
			fmt.Printf("worktree: %s\n", response.Result.WorktreePath)
		}
	}
	if response.Error != "" {
		fmt.Printf("error: %s\n", response.Error)
	}
}

func operationBranch(result *schemas.TaskResult) string {
	if result == nil {
		return ""
	}
	if result.Branch != "" {
		return result.Branch
	}
	if result.WorktreePath != "" {
		base := filepath.Base(result.WorktreePath)
		if parts := strings.SplitN(base, schemas.SimpleSessionDelimiter, 2); len(parts) == 2 && parts[1] != "" {
			return parts[1]
		}
	}
	return ""
}
