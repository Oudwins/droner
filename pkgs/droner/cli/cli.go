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

	"droner/internals/schemas"
	"droner/sdk"

	z "github.com/Oudwins/zog"
)

var ErrUsage = errors.New("usage:\n  droner new [--path <path>] [--id <id>]\n  droner del <id>")

type NewArgs struct {
	Path string `zog:"path"`
	ID   string `zog:"id"`
}

type DelArgs struct {
	ID string `zog:"id"`
}

var newArgsSchema = z.Struct(z.Shape{
	"Path": z.String().Optional().Trim(),
	"ID":   z.String().Optional().Trim(),
})

var delArgsSchema = z.Struct(z.Shape{
	"ID": z.String().Required().Trim(),
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
		response, err := client.CreateSession(ctx, schemas.SessionCreateRequest{Path: parsed.Path, SessionID: parsed.ID})
		if err != nil {
			return err
		}
		fmt.Printf("session: %s\nworktree: %s\n", response.SessionID, response.WorktreePath)
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
		fmt.Printf("session: %s\nworktree: %s\n", response.SessionID, response.WorktreePath)
		return nil
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
		default:
			return parsed, ErrUsage
		}
	}
	return parsed, nil
}

func parseDelArgs(args []string) (DelArgs, error) {
	if len(args) != 1 {
		return DelArgs{}, ErrUsage
	}
	return DelArgs{ID: args[0]}, nil
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
