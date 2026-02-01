package server

import (
	"context"
	"errors"
	"os"

	"github.com/Oudwins/droner/pkgs/droner/internals/schemas"
)

// deleteSessionBySessionID deletes a session by its session ID (used by event-driven cleanup)
func (s *Server) deleteSessionBySessionID(sessionID string) {
	worktreeRoot := s.Base.Config.Worktrees.Dir

	worktreePath, err := resolveDeleteWorktreePath(s.Base.Workspace, worktreeRoot, schemas.SessionDeleteRequest{SessionID: sessionID})
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			s.Base.Logger.Warn("Worktree not found for event-driven cleanup",
				"error", err,
				"session_id", sessionID,
			)
			return
		}
		s.Base.Logger.Warn("Worktree not found for event-driven cleanup",
			"error", err,
			"session_id", sessionID,
		)
		return
	}

	_, err = s.runDeleteSession(context.Background(), schemas.SessionDeleteRequest{SessionID: sessionID}, worktreePath)
	if err != nil {
		s.Base.Logger.Error("Failed to delete session during event-driven cleanup",
			"error", err,
			"session_id", sessionID,
		)
		return
	}

	s.Base.Logger.Info("Session deleted via event-driven cleanup",
		"session_id", sessionID,
		"worktree_path", worktreePath,
	)
}
