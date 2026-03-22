package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/Oudwins/droner/pkgs/droner/dronerd/core"
	"github.com/Oudwins/droner/pkgs/droner/dronerd/core/db"
	"github.com/Oudwins/droner/pkgs/droner/internals/backends"
	"github.com/Oudwins/droner/pkgs/droner/internals/conf"
	"github.com/Oudwins/droner/pkgs/droner/internals/schemas"
	"github.com/Oudwins/droner/pkgs/droner/internals/tasky"
)

type hydrationSummary struct {
	Total     int
	Running   int
	Completed int
	Deleted   int
	Failed    int
	Errors    int
}

func (s *Server) hydrateRunningSessions(ctx context.Context) error {
	sessions, err := s.Base.DB.ListSessionsByStatus(ctx, db.SessionStatusRunning)
	if err != nil {
		return fmt.Errorf("failed to list running sessions for hydration: %w", err)
	}

	summary := hydrationSummary{Total: len(sessions)}
	for _, session := range sessions {
		status, err := s.hydrateSession(ctx, session)
		summary.addStatus(status)
		if err != nil {
			summary.Errors++
			s.Base.Logger.Error("failed to hydrate running session", slog.String("sessionID", session.SimpleID), slog.String("error", err.Error()))
		}
	}
	s.Base.Logger.Info(
		"startup session hydration complete",
		slog.Int("total", summary.Total),
		slog.Int("running", summary.Running),
		slog.Int("completed", summary.Completed),
		slog.Int("deleted", summary.Deleted),
		slog.Int("failed", summary.Failed),
		slog.Int("errors", summary.Errors),
	)

	return nil
}

func (s *Server) hydrateSession(ctx context.Context, session db.Session) (db.SessionStatus, error) {
	agentConfig, err := s.sessionAgentConfig(session)
	if err != nil {
		result := backends.HydrationResult{
			Status: db.SessionStatusFailed,
			Error:  fmt.Sprintf("failed to parse persisted agent config: %v", err),
		}
		return result.Status, s.updateHydratedSessionStatus(ctx, session, result)
	}

	backend, err := s.Base.BackendStore.Get(conf.BackendID(session.BackendID))
	if err != nil {
		result := backends.HydrationResult{
			Status: db.SessionStatusFailed,
			Error:  fmt.Sprintf("failed to resolve backend %q: %v", session.BackendID, err),
		}
		return result.Status, s.updateHydratedSessionStatus(ctx, session, result)
	}

	result, err := backend.HydrateSession(ctx, session, agentConfig)
	if err != nil {
		result = backends.HydrationResult{
			Status: db.SessionStatusFailed,
			Error:  err.Error(),
		}
	}
	if result.Status == "" {
		result.Status = db.SessionStatusFailed
		if strings.TrimSpace(result.Error) == "" {
			result.Error = "backend returned empty hydration status"
		}
	}

	if err := s.updateHydratedSessionStatus(ctx, session, result); err != nil {
		return result.Status, err
	}
	if result.Status != db.SessionStatusRunning {
		return result.Status, nil
	}

	remoteURL := ""
	if session.RemoteUrl.Valid {
		remoteURL = strings.TrimSpace(session.RemoteUrl.String)
	}
	if remoteURL == "" {
		return result.Status, nil
	}

	if err := s.Base.SubscribeSessionRemote(context.Background(), remoteURL, session.SimpleID, func(sessionID string) {
		data, _ := json.Marshal(schemas.SessionCompleteRequest{SessionID: schemas.NewSSessionID(sessionID)})
		taskID, enqueueErr := s.Base.TaskQueue.Enqueue(context.Background(), tasky.NewTask(core.JobCompleteSession, data))
		if enqueueErr != nil {
			s.Base.Logger.Error("[hydrate session] failed to enqueue complete task", slog.String("taskId", taskID), slog.String("error", enqueueErr.Error()), slog.String("sessionId", session.SimpleID))
		}
	}); err != nil {
		s.Base.Logger.Error("[hydrate session] failed to subscribe to remote events", slog.String("error", err.Error()), slog.String("sessionId", session.SimpleID))
	}

	return result.Status, nil
}

func (s *Server) sessionAgentConfig(session db.Session) (backends.AgentConfig, error) {
	agentConfig := backends.AgentConfig{
		Model:    s.Base.Config.Sessions.Agent.DefaultModel,
		Opencode: s.Base.Config.Sessions.Agent.Providers.OpenCode,
	}
	if !session.AgentConfig.Valid || strings.TrimSpace(session.AgentConfig.String) == "" {
		return agentConfig, nil
	}

	var persisted schemas.SessionAgentConfig
	if err := json.Unmarshal([]byte(session.AgentConfig.String), &persisted); err != nil {
		return agentConfig, err
	}

	if strings.TrimSpace(persisted.Model) != "" {
		agentConfig.Model = persisted.Model
	}
	agentConfig.AgentName = persisted.AgentName
	agentConfig.Message = persisted.Message
	return agentConfig, nil
}

func (s *Server) updateHydratedSessionStatus(ctx context.Context, session db.Session, result backends.HydrationResult) error {
	status := result.Status
	if status == "" {
		status = db.SessionStatusFailed
	}

	errValue := sql.NullString{}
	if strings.TrimSpace(result.Error) != "" {
		errValue = sql.NullString{String: strings.TrimSpace(result.Error), Valid: true}
	}

	if status == session.Status && errValue.Valid == session.Error.Valid && (!errValue.Valid || errValue.String == session.Error.String) {
		return nil
	}

	_, err := s.Base.DB.UpdateSessionStatusByID(ctx, db.UpdateSessionStatusByIDParams{
		ID:     session.ID,
		Status: status,
		Error:  errValue,
	})
	if err != nil {
		return fmt.Errorf("failed to update hydrated session %s to %s: %w", session.SimpleID, status, err)
	}

	return nil
}

func (s *hydrationSummary) addStatus(status db.SessionStatus) {
	switch status {
	case db.SessionStatusRunning:
		s.Running++
	case db.SessionStatusCompleted:
		s.Completed++
	case db.SessionStatusDeleted:
		s.Deleted++
	case db.SessionStatusFailed:
		s.Failed++
	}
}
