package core

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path"
	"path/filepath"
	"strings"

	"github.com/Oudwins/droner/pkgs/droner/dronerd/core/db"
	"github.com/Oudwins/droner/pkgs/droner/internals/assert"
	"github.com/Oudwins/droner/pkgs/droner/internals/schemas"
	"github.com/Oudwins/droner/pkgs/droner/internals/tasky"
	"github.com/Oudwins/droner/pkgs/droner/internals/tasky/backends/tasky_sqlite3"
	"github.com/Oudwins/zog"
	"github.com/Oudwins/zog/parsers/zjson"
)

type Jobs string

const (
	JobCreateSession Jobs = "session_delete_job"
	JobDeleteSession Jobs = "session_create_job"
)

// file system will do weird things if we pass a sessionId with /
func sessionIdToPathIdentifier(id string) string {
	return strings.ReplaceAll(id, "/", ".") // guranteed to have no more than one / together
}

// func pathIndentifierToSessionId(id string) string {
// 	return strings.ReplaceAll(id, ".", "/")
// }

var delimiter = ".."

func NewQueue(base *BaseServer) (*tasky.Queue[Jobs], error) {

	createSessionJob := tasky.NewJob(JobCreateSession, tasky.JobConfig[Jobs]{
		Run: func(ctx context.Context, task *tasky.Task[Jobs]) error {
			ws := base.Workspace
			payload := schemas.SessionCreateRequest{}
			errs := schemas.SessionCreateSchema.Parse(
				zjson.Decode(bytes.NewReader(task.Payload)),
				&payload,
				zog.WithCtxValue("workspace", base.Workspace),
			)
			if errs != nil {
				return fmt.Errorf("[create session] failed to validate payload: %s", zog.Issues.FlattenAndCollect(errs))
			}

			repoName := filepath.Base(payload.Path)
			worktreePath := path.Join(base.Config.Worktrees.Dir, repoName+delimiter+sessionIdToPathIdentifier(payload.SessionID))

			// TODO: this needs to be idempotent. Otherwise if we fail in step beyond this one this task will fail forever
			if err := ws.CreateGitWorktree(payload.Path, worktreePath, payload.SessionID); err != nil {
				_, updateErr := base.DB.UpdateSessionStatusBySimpleID(ctx, db.UpdateSessionStatusBySimpleIDParams{
					SimpleID: payload.SessionID,
					Status:   db.SessionStatusFailed,
					Error:    sql.NullString{String: err.Error(), Valid: true},
				})
				if updateErr != nil {
					base.Logger.Error("[create session] Failed to update session status", slog.String("taskId", task.TaskID), slog.String("error", updateErr.Error()), slog.String("sessionId", payload.SessionID))
				}
				return err
			}

			// create tmux sesion
			// TODO: this needs to be idempotent. Otherwise if we fail in step beyond this one this task will fail forever
			if err := ws.CreateTmuxSession(payload.SessionID, worktreePath, payload.Agent.Model, payload.Agent.Prompt); err != nil {
				base.Logger.Error("[create session] Failed to create tmux session", slog.String("taskId", task.TaskID), slog.String("error", err.Error()))
				_, updateErr := base.DB.UpdateSessionStatusBySimpleID(ctx, db.UpdateSessionStatusBySimpleIDParams{
					SimpleID: payload.SessionID,
					Status:   db.SessionStatusFailed,
					Error:    sql.NullString{String: err.Error(), Valid: true},
				})
				if updateErr != nil {
					base.Logger.Error("[create session] Failed to update session status", slog.String("taskId", task.TaskID), slog.String("error", updateErr.Error()), slog.String("sessionId", payload.SessionID))
				}
				return err
			}

			if remoteURL, err := ws.GetRemoteURL(payload.Path); err != nil {
				err := base.Subscriptions.subscribe(context.Background(), remoteURL, payload.SessionID, func(sessionId string) {
					data, _ := json.Marshal(schemas.SessionDeleteRequest{SessionID: sessionId})
					taskId, err := base.TaskQueue.Enqueue(context.Background(), tasky.NewTask(JobDeleteSession, data))
					if err != nil {
						base.Logger.Error("[create session] Failed to enque task", slog.String("taskId", taskId), slog.String("error", err.Error()), slog.String("sessionId", payload.SessionID))
					}
				})
				if err != nil {
					base.Logger.Error("[create session] Failed to subscribe to remote events", slog.String("taskId", task.TaskID), slog.String("error", err.Error()), slog.String("sessionId", payload.SessionID))
				}
			}

			_, err := base.DB.UpdateSessionStatusBySimpleID(ctx, db.UpdateSessionStatusBySimpleIDParams{
				SimpleID: payload.SessionID,
				Status:   db.SessionStatusRunning,
				Error:    sql.NullString{},
			})
			if err != nil {
				base.Logger.Error("[create session] Failed to update session status", slog.String("taskId", task.TaskID), slog.String("error", err.Error()), slog.String("sessionId", payload.SessionID))
				return err
			}

			return nil
		},
	})

	deleteSessionJob := tasky.NewJob(JobDeleteSession, tasky.JobConfig[Jobs]{
		Run: func(ctx context.Context, task *tasky.Task[Jobs]) error {
			ws := base.Workspace
			data := schemas.SessionDeleteRequest{}
			errs := schemas.SessionDeleteSchema.Parse(zjson.Decode(bytes.NewReader(task.Payload)), &data)
			if errs != nil {
				return fmt.Errorf("[delete sesssion] failed to validate payload: %s", zog.Issues.FlattenAndCollect(errs))
			}

			session, err := base.DB.GetSessionBySimpleID(ctx, data.SessionID)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return nil // no op
				}
				return fmt.Errorf("[delete session] failed to load session: %w", err)
			}

			worktreePath := session.WorktreePath
			commonGitDir, err := ws.GitCommonDirFromWorktree(worktreePath)
			if err != nil {
				return err
			}
			worktreeName := data.SessionID

			if err := ws.KillTmuxSession(worktreeName); err != nil {
				_, updateErr := base.DB.UpdateSessionStatusBySimpleID(ctx, db.UpdateSessionStatusBySimpleIDParams{
					SimpleID: data.SessionID,
					Status:   db.SessionStatusFailed,
					Error:    sql.NullString{String: err.Error(), Valid: true},
				})
				if updateErr != nil {
					base.Logger.Error("[delete session] Failed to update session status", slog.String("taskId", task.TaskID), slog.String("error", updateErr.Error()), slog.String("sessionId", data.SessionID))
				}
				return err
			}
			if err := ws.RemoveGitWorktree(worktreePath); err != nil {
				_, updateErr := base.DB.UpdateSessionStatusBySimpleID(ctx, db.UpdateSessionStatusBySimpleIDParams{
					SimpleID: data.SessionID,
					Status:   db.SessionStatusFailed,
					Error:    sql.NullString{String: err.Error(), Valid: true},
				})
				if updateErr != nil {
					base.Logger.Error("[queue] Failed to update session status", slog.String("taskId", task.TaskID), slog.String("error", updateErr.Error()), slog.String("sessionId", data.SessionID))
				}
				return err
			}
			if err := ws.DeleteGitBranch(commonGitDir, data.SessionID); err != nil {
				_, updateErr := base.DB.UpdateSessionStatusBySimpleID(ctx, db.UpdateSessionStatusBySimpleIDParams{
					SimpleID: data.SessionID,
					Status:   db.SessionStatusFailed,
					Error:    sql.NullString{String: err.Error(), Valid: true},
				})
				if updateErr != nil {
					base.Logger.Error("[queue] Failed to update session status", slog.String("taskId", task.TaskID), slog.String("error", updateErr.Error()), slog.String("sessionId", data.SessionID))
				}
				return err
			}

			_, err = base.DB.UpdateSessionStatusBySimpleID(ctx, db.UpdateSessionStatusBySimpleIDParams{
				SimpleID: data.SessionID,
				Status:   db.SessionStatusDeleted, // TODO: Might want to set this to completed in the future. So I can reuse the worktrees
				Error:    sql.NullString{},
			})
			if err != nil {
				base.Logger.Error("[queue] Failed to update session status", slog.String("taskId", task.TaskID), slog.String("error", err.Error()), slog.String("sessionId", data.SessionID))
				return err
			}
			return nil
		},
	})

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
