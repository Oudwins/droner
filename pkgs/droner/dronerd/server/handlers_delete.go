package server

import (
	"context"
	"path/filepath"
)

// deleteSessionBySessionID deletes a session by its session ID (used by event-driven cleanup)
func (s *Server) deleteSessionBySessionID(sessionID string) {
	worktreeRoot, err := expandPath(s.Config.WORKTREE_DIR)
	if err != nil {
		s.Logger.Error("Failed to expand worktree root for event-driven cleanup",
			"error", err,
			"session_id", sessionID,
		)
		return
	}

	worktreePath, err := findWorktreeBySessionID(worktreeRoot, sessionID)
	if err != nil {
		s.Logger.Warn("Worktree not found for event-driven cleanup",
			"error", err,
			"session_id", sessionID,
		)
		return
	}

	worktreeName := filepath.Base(worktreePath)
	commonGitDir, err := gitCommonDirFromWorktree(worktreePath)
	if err != nil {
		s.Logger.Error("Failed to get git common dir for event-driven cleanup",
			"error", err,
			"session_id", sessionID,
		)
		return
	}

	// Unsubscribe from remote events first
	if remoteURL, err := getRemoteURLFromWorktree(worktreePath); err == nil {
		if err := s.subs.unsubscribe(context.Background(), remoteURL, sessionID, s.Logger); err != nil {
			s.Logger.Warn("Failed to unsubscribe from remote events during cleanup",
				"error", err,
				"remote_url", remoteURL,
				"session_id", sessionID,
			)
		}
	}

	// Perform cleanup steps
	if err := killTmuxSession(worktreeName); err != nil {
		s.Logger.Error("Failed to kill tmux session during event-driven cleanup",
			"error", err,
			"session_id", sessionID,
		)
	}

	if err := removeGitWorktree(worktreePath); err != nil {
		s.Logger.Error("Failed to remove git worktree during event-driven cleanup",
			"error", err,
			"session_id", sessionID,
		)
	}

	if err := deleteGitBranch(commonGitDir, sessionID); err != nil {
		s.Logger.Error("Failed to delete git branch during event-driven cleanup",
			"error", err,
			"session_id", sessionID,
		)
	}

	s.Logger.Info("Session deleted via event-driven cleanup",
		"session_id", sessionID,
		"worktree_path", worktreePath,
	)
}
