package sessions

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestDeriveNamesSanitizesPhysicalNames(t *testing.T) {
	t.Parallel()

	tmuxSessionName, worktreePath, err := DeriveNames("/tmp/droner", "/tmp/worktrees", "feature/foo")
	if err != nil {
		t.Fatalf("DeriveNames returned error: %v", err)
	}
	if strings.Contains(tmuxSessionName, "/") {
		t.Fatalf("TmuxSessionName contains slash: %q", tmuxSessionName)
	}
	if strings.Contains(filepath.Base(worktreePath), "/") {
		t.Fatalf("WorktreePath base contains slash: %q", worktreePath)
	}
	if !strings.HasPrefix(tmuxSessionName, "droner#feature-foo-") {
		t.Fatalf("TmuxSessionName = %q, want sanitized hashed prefix", tmuxSessionName)
	}
	if !strings.HasPrefix(filepath.Base(worktreePath), "droner..feature-foo-") {
		t.Fatalf("WorktreePath = %q, want sanitized hashed leaf", worktreePath)
	}
}

func TestDeriveNamesKeepsSafeBranchPhysicalName(t *testing.T) {
	t.Parallel()

	tmuxSessionName, worktreePath, err := DeriveNames("/tmp/droner", "/tmp/worktrees", "fix-crash")
	if err != nil {
		t.Fatalf("DeriveNames returned error: %v", err)
	}
	if tmuxSessionName != "droner#fix-crash" {
		t.Fatalf("TmuxSessionName = %q, want droner#fix-crash", tmuxSessionName)
	}
	if filepath.Base(worktreePath) != "droner..fix-crash" {
		t.Fatalf("WorktreePath base = %q, want droner..fix-crash", filepath.Base(worktreePath))
	}
}

func TestDeriveNamesRemovesDelimitersBeforeAddingThem(t *testing.T) {
	t.Parallel()

	tmuxSessionName, worktreePath, err := DeriveNames("/tmp/drone..repo#api", "/tmp/worktrees", "feature/foo#bar..baz")
	if err != nil {
		t.Fatalf("DeriveNames returned error: %v", err)
	}

	tmuxParts := strings.Split(tmuxSessionName, "#")
	if len(tmuxParts) != 2 {
		t.Fatalf("TmuxSessionName = %q, want exactly one # delimiter", tmuxSessionName)
	}
	worktreeParts := strings.Split(filepath.Base(worktreePath), "..")
	if len(worktreeParts) != 2 {
		t.Fatalf("WorktreePath = %q, want exactly one .. delimiter", worktreePath)
	}
}
