package cliutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Oudwins/droner/pkgs/droner/internals/conf"
)

func TestParseRepoLocator(t *testing.T) {
	tests := []struct {
		name       string
		raw        string
		wantRepo   string
		wantBranch string
		wantErr    bool
	}{
		{name: "empty", raw: "", wantRepo: "", wantBranch: ""},
		{name: "repo and branch", raw: "repo@feature", wantRepo: "repo", wantBranch: "feature"},
		{name: "repo only", raw: "repo", wantRepo: "repo", wantBranch: ""},
		{name: "branch only", raw: "@feature", wantRepo: "", wantBranch: "feature"},
		{name: "invalid empty locator", raw: "@", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, branch, err := parseRepoLocator(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("parseRepoLocator: %v", err)
			}
			if repo != tt.wantRepo || branch != tt.wantBranch {
				t.Fatalf("got (%q, %q), want (%q, %q)", repo, branch, tt.wantRepo, tt.wantBranch)
			}
		})
	}
}

func TestResolveProjectRepoMatchesDirectChildGitRepo(t *testing.T) {
	parentDir := t.TempDir()
	repoDir := filepath.Join(parentDir, "repo")
	initGitRepo(t, repoDir)

	config := conf.GetConfig()
	orig := append([]string(nil), config.Projects.ParentPaths...)
	config.Projects.ParentPaths = []string{parentDir}
	t.Cleanup(func() { config.Projects.ParentPaths = orig })

	got, err := resolveProjectRepo("repo")
	if err != nil {
		t.Fatalf("resolveProjectRepo: %v", err)
	}
	if got != repoDir {
		t.Fatalf("repoPath = %q, want %q", got, repoDir)
	}
}

func TestResolveProjectRepoRejectsNestedPath(t *testing.T) {
	if _, err := resolveProjectRepo("org/repo"); err == nil {
		t.Fatal("expected error")
	}
}

func TestResolveProjectRepoReturnsAmbiguousError(t *testing.T) {
	root := t.TempDir()
	firstParent := filepath.Join(root, "first")
	secondParent := filepath.Join(root, "second")
	if err := os.MkdirAll(filepath.Join(firstParent, "repo"), 0o755); err != nil {
		t.Fatalf("MkdirAll first: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(secondParent, "repo"), 0o755); err != nil {
		t.Fatalf("MkdirAll second: %v", err)
	}
	initGitRepo(t, filepath.Join(firstParent, "repo"))
	initGitRepo(t, filepath.Join(secondParent, "repo"))

	config := conf.GetConfig()
	orig := append([]string(nil), config.Projects.ParentPaths...)
	config.Projects.ParentPaths = []string{firstParent, secondParent}
	t.Cleanup(func() { config.Projects.ParentPaths = orig })

	if _, err := resolveProjectRepo("repo"); err == nil {
		t.Fatal("expected ambiguity error")
	}
}

func TestResolveSessionTargetUsesCurrentRepoForBranchOnly(t *testing.T) {
	repoDir := t.TempDir()
	initGitRepo(t, repoDir)

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(repoDir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(wd)
	})

	target, err := ResolveSessionTarget("@feature")
	if err != nil {
		t.Fatalf("ResolveSessionTarget: %v", err)
	}
	if target.RepoPath != repoDir {
		t.Fatalf("repoPath = %q, want %q", target.RepoPath, repoDir)
	}
	if target.Branch != "feature" {
		t.Fatalf("branch = %q, want %q", target.Branch, "feature")
	}
}

func initGitRepo(t *testing.T, repoDir string) {
	t.Helper()
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("MkdirAll repo: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	runGit(t, repoDir, "init")
	runGit(t, repoDir, "add", "README.md")
	runGit(t, repoDir, "-c", "user.name=Test User", "-c", "user.email=test@example.com", "commit", "-m", "init")
}

func runGit(t *testing.T, repoDir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repoDir}, args...)...)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, string(output))
	}
}
