package workspace

import "os"

type Host interface {
	Stat(path string) (os.FileInfo, error)
	ReadDir(path string) ([]os.DirEntry, error)
	ReadFile(path string) ([]byte, error)
	MkdirAll(path string, perm os.FileMode) error

	GitIsInsideWorkTree(repoPath string) error
	CreateGitWorktree(sessionID string, repoPath string, worktreePath string) error
	RemoveGitWorktree(worktreePath string) error
	GitCommonDirFromWorktree(worktreePath string) (string, error)
	DeleteGitBranch(commonGitDir string, sessionID string) error
	GetRemoteURL(repoPath string) (string, error)
	GetRemoteURLFromWorktree(worktreePath string) (string, error)
	RunWorktreeSetup(repoPath string, worktreePath string) error
	CreateTmuxSession(sessionName string, worktreePath string, model string, prompt string) error
	KillTmuxSession(sessionName string) error
}
