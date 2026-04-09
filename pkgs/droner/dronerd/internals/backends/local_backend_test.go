package backends

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Oudwins/droner/pkgs/droner/internals/conf"
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

func TestLocalBackendCreateGitWorktreeUsesExistingLocalBranch(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "cmd.log")
	repoPath := filepath.Join(t.TempDir(), "repo")
	worktreePath := filepath.Join(t.TempDir(), "worktree")
	useBackendHelperProcess(t, logPath, []string{"refs/heads/feature"})

	backend := LocalBackend{}
	if err := backend.createGitWorktree(repoPath, worktreePath, "feature", localBranchState{localExists: true}); err != nil {
		t.Fatalf("createGitWorktree: %v", err)
	}

	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(raw), "git\t-C\t"+repoPath+"\tworktree\tadd\t--force\t"+worktreePath+"\tfeature") {
		t.Fatalf("expected existing-branch worktree add command, log:\n%s", string(raw))
	}
}

func TestLocalBackendCreateGitWorktreeUsesRemoteBranchAsBase(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "cmd.log")
	repoPath := filepath.Join(t.TempDir(), "repo")
	worktreePath := filepath.Join(t.TempDir(), "worktree")
	useBackendHelperProcess(t, logPath, []string{"refs/remotes/origin/feature"})

	backend := LocalBackend{}
	if err := backend.createGitWorktree(repoPath, worktreePath, "feature", localBranchState{remoteRef: "refs/remotes/origin/feature"}); err != nil {
		t.Fatalf("createGitWorktree: %v", err)
	}

	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(raw), "git\t-C\t"+repoPath+"\tworktree\tadd\t-b\tfeature\t"+worktreePath+"\trefs/remotes/origin/feature") {
		t.Fatalf("expected remote-branch worktree add command, log:\n%s", string(raw))
	}
}

func TestLocalBackendPrepareSessionWorktreeReusesExistingTargetPath(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "cmd.log")
	worktreePath := filepath.Join(t.TempDir(), "worktree")
	if err := os.MkdirAll(worktreePath, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	useBackendHelperProcess(t, logPath, nil)

	backend := LocalBackend{}
	reused, cleanupCandidate, err := backend.prepareSessionWorktree(context.Background(), "/repo", worktreePath, "feature", CreateSessionOptions{
		LookupWorktreeSession: func(context.Context, string) (*WorktreeSessionRef, error) {
			return &WorktreeSessionRef{StreamID: "old", Branch: "feature", PublicState: "completed"}, nil
		},
	}, localBranchState{localExists: true}, true)
	if err != nil {
		t.Fatalf("prepareSessionWorktree: %v", err)
	}
	if !reused {
		t.Fatal("expected worktree reuse")
	}
	if cleanupCandidate != nil {
		t.Fatalf("expected no cleanup candidate, got %#v", cleanupCandidate)
	}

	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	log := string(raw)
	if strings.Contains(log, "\tworktree\tadd\t") {
		t.Fatalf("did not expect worktree add, log:\n%s", log)
	}
	if !strings.Contains(log, "git\t-C\t"+worktreePath+"\treset\t--hard") || !strings.Contains(log, "git\t-C\t"+worktreePath+"\tcheckout\tfeature") {
		t.Fatalf("expected in-place reuse commands, log:\n%s", log)
	}
}

func TestLocalBackendPrepareSessionWorktreeBlocksLiveExistingTargetPath(t *testing.T) {
	worktreePath := filepath.Join(t.TempDir(), "worktree")
	if err := os.MkdirAll(worktreePath, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	backend := LocalBackend{}
	_, _, err := backend.prepareSessionWorktree(context.Background(), "/repo", worktreePath, "feature", CreateSessionOptions{
		LookupWorktreeSession: func(context.Context, string) (*WorktreeSessionRef, error) {
			return &WorktreeSessionRef{StreamID: "live-stream", Branch: "feature", PublicState: "active.idle"}, nil
		},
	}, localBranchState{localExists: true}, true)
	if err == nil || !strings.Contains(err.Error(), "status=active.idle") || !strings.Contains(err.Error(), "streamID=live-stream") {
		t.Fatalf("expected blocked worktree error, got %v", err)
	}
}

func TestLocalBackendPrepareSessionWorktreeIgnoresCurrentStreamCollision(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "cmd.log")
	worktreePath := filepath.Join(t.TempDir(), "worktree")
	if err := os.MkdirAll(worktreePath, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	useBackendHelperProcess(t, logPath, nil)

	backend := LocalBackend{}
	reused, cleanupCandidate, err := backend.prepareSessionWorktree(context.Background(), "/repo", worktreePath, "feature", CreateSessionOptions{
		CurrentStreamID: "current-stream",
		LookupWorktreeSession: func(context.Context, string) (*WorktreeSessionRef, error) {
			return &WorktreeSessionRef{StreamID: "current-stream", Branch: "feature", PublicState: "queued"}, nil
		},
	}, localBranchState{localExists: true}, true)
	if err != nil {
		t.Fatalf("prepareSessionWorktree: %v", err)
	}
	if !reused {
		t.Fatal("expected self-collision to be ignored and reused")
	}
	if cleanupCandidate != nil {
		t.Fatalf("expected no cleanup candidate, got %#v", cleanupCandidate)
	}
}

func TestLocalBackendPrepareSessionWorktreeReusesRepoCandidateBeforeCreatingFresh(t *testing.T) {
	root := t.TempDir()
	logPath := filepath.Join(root, "cmd.log")
	worktreeRoot := filepath.Join(root, "worktrees")
	oldWorktreePath := filepath.Join(worktreeRoot, "repo..old")
	targetWorktreePath := filepath.Join(worktreeRoot, "repo..feature")
	if err := os.MkdirAll(oldWorktreePath, 0o755); err != nil {
		t.Fatalf("MkdirAll old worktree: %v", err)
	}
	useBackendHelperProcess(t, logPath, nil)

	backend := LocalBackend{config: &conf.LocalBackendConfig{WorktreeDir: worktreeRoot}}
	reused, cleanupCandidate, err := backend.prepareSessionWorktree(context.Background(), "/repo", targetWorktreePath, "feature", CreateSessionOptions{
		NextReusableWorktree: func(context.Context) (*ReusableWorktreeCandidate, error) {
			candidate := &ReusableWorktreeCandidate{StreamID: "old-stream", Branch: "old", RepoPath: "/repo", WorktreePath: oldWorktreePath}
			return candidate, nil
		},
	}, localBranchState{localExists: true}, false)
	if err != nil {
		t.Fatalf("prepareSessionWorktree: %v", err)
	}
	if !reused {
		t.Fatal("expected candidate reuse")
	}
	if cleanupCandidate == nil || cleanupCandidate.StreamID != "old-stream" {
		t.Fatalf("unexpected cleanup candidate: %#v", cleanupCandidate)
	}

	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	log := string(raw)
	if !strings.Contains(log, "git\t-C\t/repo\tworktree\tmove\t"+oldWorktreePath+"\t"+targetWorktreePath) {
		t.Fatalf("expected worktree move, log:\n%s", log)
	}
	if strings.Contains(log, "\tworktree\tadd\t") {
		t.Fatalf("did not expect fresh worktree add, log:\n%s", log)
	}
}

func useBackendHelperProcess(t *testing.T, logPath string, existingRefs []string) {
	t.Helper()
	origExec := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		cmdArgs := append([]string{"-test.run=TestHelperProcess", "--", name}, args...)
		cmd := exec.Command(os.Args[0], cmdArgs...)
		cmd.Env = append(os.Environ(),
			"GO_WANT_HELPER_PROCESS=1",
			"DRONER_TEST_CMD_LOG="+logPath,
			"DRONER_TEST_EXISTING_REFS="+strings.Join(existingRefs, ","),
		)
		return cmd
	}
	t.Cleanup(func() { execCommand = origExec })
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

	if name == "git" && len(args) >= 6 && args[2] == "show-ref" && args[3] == "--verify" && args[4] == "--quiet" {
		existingRefs := strings.Split(os.Getenv("DRONER_TEST_EXISTING_REFS"), ",")
		for _, ref := range existingRefs {
			if ref == args[5] {
				os.Exit(0)
			}
		}
		os.Exit(1)
	}

	if name == "git" && len(args) >= 3 && args[2] == "rev-parse" {
		_, _ = os.Stdout.Write([]byte(".git\n"))
		os.Exit(0)
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
