package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path"

	"github.com/Oudwins/droner/pkgs/droner/dronerd/core/db"
	"github.com/Oudwins/droner/pkgs/droner/internals/assert"
	"github.com/Oudwins/droner/pkgs/droner/internals/backends"
	"github.com/Oudwins/droner/pkgs/droner/internals/schemas"
	"github.com/Oudwins/droner/pkgs/droner/internals/tasky"
	"github.com/Oudwins/droner/pkgs/droner/internals/tasky/backends/tasky_sqlite3"
	"github.com/Oudwins/zog"
)

type Jobs string

const (
	JobCreateSession     Jobs = "session_delete_job"
	JobDeleteSession     Jobs = "session_create_job"
	JobDeleteAllSessions Jobs = "session_nuke_job"
)

func NewQueue(base *BaseServer) (*tasky.Queue[Jobs], error) {

	createSessionJob := tasky.NewJob(JobCreateSession, tasky.JobConfig[Jobs]{
		Run: func(ctx context.Context, task *tasky.Task[Jobs]) error {
			logger := base.Logger.With(slog.String("taskId", task.TaskID), slog.String("jobId", string(task.JobID)))
			payload := schemas.SessionCreateRequest{}
			err2 := json.Unmarshal(task.Payload, &payload)
			if err2 != nil {
				logger.Error("Failed to unmarshal payload", slog.String("error", err2.Error()))
				return err2
			}
			logger = logger.With(slog.Any("payload", payload))
			errs := schemas.SessionCreateSchema.Validate(&payload)
			if errs != nil {
				return fmt.Errorf("failed to validate payload: %s", zog.Issues.FlattenAndCollect(errs))
			}

			logger.Debug("Starting with payload")

			backend, err := base.BackendStore.Get(payload.BackendID)
			if err != nil {
				return fmt.Errorf("failed to resolve backend: %w", err)
			}
			worktreePath, err := backend.WorktreePath(payload.Path, payload.SessionID.String())
			if err != nil {
				return fmt.Errorf("failed to resolve worktree path: %w", err)
			}

			model := base.Config.Sessions.Agent.DefaultModel
			prompt := ""
			if payload.AgentConfig != nil {
				if payload.AgentConfig.Model != "" {
					model = payload.AgentConfig.Model
				}
				prompt = payload.AgentConfig.Prompt
			}

			// TODO: this needs to be idempotent. Otherwise if we fail in step beyond this one this task will fail forever
			if err := backend.CreateSession(ctx, payload.Path, worktreePath, payload.SessionID.String(), backends.AgentConfig{Model: model, Prompt: prompt}); err != nil {
				logger.Error("Failed to create worktree", slog.String("error", err.Error()))
				_, updateErr := base.DB.UpdateSessionStatusBySimpleID(ctx, db.UpdateSessionStatusBySimpleIDParams{
					SimpleID: payload.SessionID.String(),
					Status:   db.SessionStatusFailed,
					Error:    sql.NullString{String: err.Error(), Valid: true},
				})
				if updateErr != nil {
					logger.Error("Failed to update session status", slog.String("error", updateErr.Error()))
				}
				return err
			}

			// if remoteURL, err := ws.GetRemoteURL(payload.Path); err != nil {
			// 	err := base.Subscriptions.subscribe(context.Background(), remoteURL, payload.SessionID, func(sessionId string) {
			// 		data, _ := json.Marshal(schemas.SessionDeleteRequest{SessionID: sessionId})
			// 		taskId, err := base.TaskQueue.Enqueue(context.Background(), tasky.NewTask(JobDeleteSession, data))
			// 		if err != nil {
			// 			base.Logger.Error("[create session] Failed to enque task", slog.String("taskId", taskId), slog.String("error", err.Error()), slog.String("sessionId", payload.SessionID))
			// 		}
			// 	})
			// 	if err != nil {
			// 		base.Logger.Error("[create session] Failed to subscribe to remote events", slog.String("taskId", task.TaskID), slog.String("error", err.Error()), slog.String("sessionId", payload.SessionID))
			// 	}
			// }

			_, err = base.DB.UpdateSessionStatusBySimpleID(ctx, db.UpdateSessionStatusBySimpleIDParams{
				SimpleID: payload.SessionID.String(),
				Status:   db.SessionStatusRunning,
				Error:    sql.NullString{},
			})
			if err != nil {
				logger.Error("Failed to update session status to running", slog.String("error", err.Error()))
				return err
			}

			logger.Debug("Success")
			return nil
		},
	})

	deleteSessionJob := tasky.NewJob(JobDeleteSession, tasky.JobConfig[Jobs]{
		Run: func(ctx context.Context, task *tasky.Task[Jobs]) error {
			logger := base.Logger.With(slog.String("taskId", task.TaskID), slog.String("jobId", string(task.JobID)))
			data := schemas.SessionDeleteRequest{}
			err2 := json.Unmarshal(task.Payload, &data)
			if err2 != nil {
				logger.Error("Failed to unmarshal payload", slog.String("error", err2.Error()))
				return err2
			}
			logger = logger.With(slog.Any("payload", data))
			errs := schemas.SessionDeleteSchema.Validate(&data)
			if errs != nil {
				logger.Error("failed to validate payload", slog.Any("issues", errs))
				return fmt.Errorf("failed to validate payload: %s", zog.Issues.FlattenAndCollect(errs))
			}

			session, err := base.DB.GetSessionBySimpleID(ctx, data.SessionID.String())
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					logger.Info("No session by that ID found in database. Exiting")
					return nil // no op
				}
				return fmt.Errorf(" failed to load session: %w", err)
			}

			backend, err := base.BackendStore.Get(backends.BackendID(session.BackendID))
			if err != nil {
				logger.Error("Failed to resolve backend", slog.String("error", err.Error()))
				return err
			}
			if err := backend.DeleteSession(ctx, session.WorktreePath, data.SessionID.String()); err != nil {
				logger.Error("Failed to cleanup session", slog.String("error", err.Error()))
				_, updateErr := base.DB.UpdateSessionStatusBySimpleID(ctx, db.UpdateSessionStatusBySimpleIDParams{
					SimpleID: data.SessionID.String(),
					Status:   db.SessionStatusFailed,
					Error:    sql.NullString{String: err.Error(), Valid: true},
				})
				if updateErr != nil {
					logger.Error("[queue] Failed to update session status", slog.String("error", updateErr.Error()))
				}
				return err
			}

			_, err = base.DB.UpdateSessionStatusBySimpleID(ctx, db.UpdateSessionStatusBySimpleIDParams{
				SimpleID: data.SessionID.String(),
				Status:   db.SessionStatusDeleted, // TODO: Might want to set this to completed in the future. So I can reuse the worktrees
				Error:    sql.NullString{},
			})
			if err != nil {
				logger.Error("Failed to update session status", slog.String("error", err.Error()))
				return err
			}
			logger.Debug("Delete session success")
			return nil
		},
	})

	nukeSessionsJob := tasky.NewJob(JobDeleteAllSessions, tasky.JobConfig[Jobs]{
		Run: func(ctx context.Context, task *tasky.Task[Jobs]) error {
			logger := base.Logger.With(slog.String("taskId", task.TaskID), slog.String("jobId", string(task.JobID)))
			ids := make([]string, 0)
			runningIDs, err := base.DB.ListRunningSessionIDs(ctx)
			if err != nil {
				logger.Error("Failed to list running sessions", slog.String("error", err.Error()))
				return err
			}
			ids = append(ids, runningIDs...)
			failures := 0
			for _, rawID := range ids {
				if rawID == "" {
					continue
				}
				sessionID := schemas.NewSSessionID(rawID)
				payload, err := json.Marshal(schemas.SessionDeleteRequest{SessionID: sessionID})
				if err != nil {
					logger.Error("Failed to serialize session delete payload", slog.String("error", err.Error()))
					failures++
					continue
				}
				_, err = base.TaskQueue.Enqueue(ctx, tasky.NewTask(JobDeleteSession, payload))
				if err != nil {
					logger.Error("Failed to enqueue delete session task", slog.String("sessionId", sessionID.String()), slog.String("error", err.Error()))
					failures++
					continue
				}
			}
			logger.Debug("Nuke sessions success", slog.Int("failures", failures))
			return nil
		},
	})

	basePath := path.Join(base.Config.Server.DataDir, "queue")
	err := os.MkdirAll(basePath, 0o755)
	assert.AssertNil(err, "[QUEUE] Failed to initialize. Could not create dirs", err)

	sqliteBackend, err := taskysqlite3.New[Jobs](taskysqlite3.Config{
		Path:      path.Join(base.Config.Server.DataDir, "queue/queue.db"),
		QueueName: "droner_queue",
	})

	assert.AssertNil(err, "[QUEUE] Failed to initialize")

	q, err := tasky.NewQueue(
		tasky.QueueConfig[Jobs]{
			Jobs:    []tasky.Job[Jobs]{createSessionJob, deleteSessionJob, nukeSessionsJob},
			Backend: sqliteBackend,
			OnError: func(err error, task *tasky.Task[Jobs], payload []byte) error {
				base.Logger.Error("[QUEUE] Task failed to complete", slog.String("taskId", task.TaskID), slog.String("jobId", string(task.JobID)), slog.String("error", err.Error()))
				return nil
			},
		},
	)

	assert.AssertNil(err, "[QUEUE] Failed to initialize")
	return q, nil
}
