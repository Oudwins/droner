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
	"path/filepath"

	"github.com/Oudwins/droner/pkgs/droner/dronerd/core/db"
	"github.com/Oudwins/droner/pkgs/droner/internals/assert"
	"github.com/Oudwins/droner/pkgs/droner/internals/schemas"
	"github.com/Oudwins/droner/pkgs/droner/internals/tasky"
	"github.com/Oudwins/droner/pkgs/droner/internals/tasky/backends/tasky_sqlite3"
	"github.com/Oudwins/zog"
)

type Jobs string

const (
	JobCreateSession Jobs = "session_delete_job"
	JobDeleteSession Jobs = "session_create_job"
)

func NewQueue(base *BaseServer) (*tasky.Queue[Jobs], error) {

	createSessionJob := tasky.NewJob(JobCreateSession, tasky.JobConfig[Jobs]{
		Run: func(ctx context.Context, task *tasky.Task[Jobs]) error {
			logger := base.Logger.With(slog.String("taskId", task.TaskID), slog.String("jobId", string(task.JobID)))
			ws := base.Workspace
			payload := schemas.SessionCreateRequest{}
			err2 := json.Unmarshal(task.Payload, &payload)
			if err2 != nil {
				logger.Error("Failed to unmarshal payload", slog.String("error", err2.Error()))
				return err2
			}
			logger = logger.With(slog.Any("payload", payload))
			errs := schemas.SessionCreateSchema.Validate(
				&payload,
				zog.WithCtxValue("workspace", base.Workspace),
			)
			if errs != nil {
				return fmt.Errorf("failed to validate payload: %s", zog.Issues.FlattenAndCollect(errs))
			}

			logger.Debug("Starting with payload")

			repoName := filepath.Base(payload.Path)
			worktreePath := path.Join(base.Config.Worktrees.Dir, payload.SessionID.SessionWorktreeName(repoName))

			// TODO: this needs to be idempotent. Otherwise if we fail in step beyond this one this task will fail forever
			if err := ws.CreateGitWorktree(payload.Path, worktreePath, payload.SessionID.String()); err != nil {
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

			// create tmux sesion
			// TODO: this needs to be idempotent. Otherwise if we fail in step beyond this one this task will fail forever
			model := base.Config.Agent.DefaultModel
			prompt := ""
			if payload.Agent != nil {
				if payload.Agent.Model != "" {
					model = payload.Agent.Model
				}
				prompt = payload.Agent.Prompt
			}
			if err := ws.CreateTmuxSession(payload.SessionID.String(), worktreePath, model, prompt); err != nil {
				logger.Error("Failed to create tmux session", slog.String("error", err.Error()))
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

			_, err := base.DB.UpdateSessionStatusBySimpleID(ctx, db.UpdateSessionStatusBySimpleIDParams{
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
			ws := base.Workspace
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

			worktreePath := session.WorktreePath
			commonGitDir, err := ws.GitCommonDirFromWorktree(worktreePath)
			if err != nil {
				logger.Error("Failed to get common dir from worktree", slog.String("error", err.Error()))
				return err
			}
			tmuxSessionName := data.SessionID.String()

			if err := ws.KillTmuxSession(tmuxSessionName); err != nil {
				logger.Error("Failed to kill tmux session", slog.String("error", err.Error()))
				_, updateErr := base.DB.UpdateSessionStatusBySimpleID(ctx, db.UpdateSessionStatusBySimpleIDParams{
					SimpleID: data.SessionID.String(),
					Status:   db.SessionStatusFailed,
					Error:    sql.NullString{String: err.Error(), Valid: true},
				})
				if updateErr != nil {
					logger.Error("Failed to update session status", slog.String("error", err.Error()))
				}
				return err
			}
			if err := ws.RemoveGitWorktree(worktreePath); err != nil {
				_, updateErr := base.DB.UpdateSessionStatusBySimpleID(ctx, db.UpdateSessionStatusBySimpleIDParams{
					SimpleID: data.SessionID.String(),
					Status:   db.SessionStatusFailed,
					Error:    sql.NullString{String: err.Error(), Valid: true},
				})
				if updateErr != nil {
					logger.Error("Failed to update session status", slog.String("error", err.Error()))
				}
				return err
			}
			if err := ws.DeleteGitBranch(commonGitDir, data.SessionID.String()); err != nil {
				logger.Error("Failed to delete git branch", slog.String("error", err.Error()))
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
			Jobs:    []tasky.Job[Jobs]{createSessionJob, deleteSessionJob},
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
