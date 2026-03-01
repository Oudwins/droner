package backends

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalBackendCompleteSessionKillsTmuxSessionNameFromWorktreePath(t *testing.T) {
	// This test ensures CompleteSession kills the same tmux session name that
	// CreateSession/DeleteSession use: `<repo>#<sessionID>` derived from the
	// worktree folder name `<repo>..<sessionID>`.
	//
	// Regression: CompleteSession used to call killTmuxSession(sessionID), which
	// doesn't match the real tmux session name and leaves tmux sessions running.
	worktreePath := "/home/tmx/.droner/worktrees/droner..readme-local-dev-ljd-17"
	sessionID := "readme-local-dev-ljd-17"
	expected := "droner#readme-local-dev-ljd-17"

	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "cmd.log")

	origExec := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		cmdArgs := append([]string{"-test.run=TestHelperProcess", "--", name}, args...)
		cmd := exec.Command(os.Args[0], cmdArgs...)
		cmd.Env = append(os.Environ(),
			"GO_WANT_HELPER_PROCESS=1",
			"DRONER_TEST_CMD_LOG="+logPath,
		)
		return cmd
	}
	t.Cleanup(func() { execCommand = origExec })

	backend := LocalBackend{}
	if err := backend.CompleteSession(context.Background(), worktreePath, sessionID); err != nil {
		t.Fatalf("CompleteSession returned error: %v", err)
	}

	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read command log: %v", err)
	}
	lines := strings.Split(string(raw), "\n")

	seenHas := false
	seenKill := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 2 {
			continue
		}
		name := fields[0]
		subcmd := fields[1]

		getTarget := func() string {
			for i := 2; i+1 < len(fields); i++ {
				if fields[i] == "-t" {
					return fields[i+1]
				}
			}
			return ""
		}

		if name == "tmux" && subcmd == "has-session" {
			seenHas = true
			if got := getTarget(); got != expected {
				t.Fatalf("expected has-session to target %q, got %q (log line: %s)", expected, got, line)
			}
		}
		if name == "tmux" && subcmd == "kill-session" {
			seenKill = true
			if got := getTarget(); got != expected {
				t.Fatalf("expected kill-session to target %q, got %q (log line: %s)", expected, got, line)
			}
		}
	}

	if !seenHas {
		t.Fatalf("expected tmux has-session to be invoked; log:\n%s", string(raw))
	}
	if !seenKill {
		t.Fatalf("expected tmux kill-session to be invoked; log:\n%s", string(raw))
	}
}

// TestHelperProcess is a helper subprocess used to stub exec.Command calls.
//
// It logs each invocation to DRONER_TEST_CMD_LOG as tab-delimited fields:
// <name>\t<arg0>\t<arg1>...\t\n
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}

	// Find args after "--".
	idx := -1
	for i, a := range os.Args {
		if a == "--" {
			idx = i
			break
		}
	}
	if idx == -1 || idx+1 >= len(os.Args) {
		os.Exit(2)
	}

	name := os.Args[idx+1]
	args := os.Args[idx+2:]

	logPath := os.Getenv("DRONER_TEST_CMD_LOG")
	if strings.TrimSpace(logPath) == "" {
		os.Exit(2)
	}
	line := fmt.Sprintf("%s\t%s\t\n", name, strings.Join(args, "\t"))
	_ = os.WriteFile(logPath, append(readFileOrEmpty(logPath), []byte(line)...), 0o644)

	// Simulate tmux behavior.
	if name == "tmux" && len(args) > 0 {
		switch args[0] {
		case "has-session":
			os.Exit(0)
		case "kill-session":
			os.Exit(0)
		default:
			os.Exit(0)
		}
	}

	os.Exit(0)
}

func readFileOrEmpty(path string) []byte {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return b
}
