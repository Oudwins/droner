package core

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"path"
	"path/filepath"

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

func NewQueue(base *BaseServer) (*tasky.Queue[Jobs], error) {

	createSessionJob := tasky.NewJob(JobCreateSession, tasky.JobConfig[Jobs]{
		Run: func(ctx context.Context, task *tasky.Task[Jobs]) error {
			ws := base.Workspace
			payload := schemas.SessionCreateRequest{}
			errs := schemas.SessionCreateSchema.Parse(zjson.Decode(bytes.NewReader(task.Payload)), &payload)
			if errs != nil {
				return fmt.Errorf("[queue] failed to validate payload: %s", zog.Issues.FlattenAndCollect(errs))
			}

			repoName := filepath.Base(payload.Path)
			worktreePath := path.Join(base.Config.Worktrees.Dir, repoName+"."+payload.SessionID)

			// TODO: this needs to be idempotent. Otherwise if we fail in step beyond this one this task will fail forever
			if err := ws.CreateGitWorktree(payload.Path, worktreePath, payload.SessionID); err != nil {
				return err
			}

			// create tmux sesion
			// TODO: this needs to be idempotent. Otherwise if we fail in step beyond this one this task will fail forever
			if err := ws.CreateTmuxSession(payload.SessionID, worktreePath, payload.Agent.Model, payload.Agent.Prompt); err != nil {
				base.Logger.Error("[queue] Failed to create tmux session", slog.String("taskId", task.TaskID), slog.String("error", err.Error()))
				return err
			}

			// TODO: try to subscribe for updates to this branch. Delete it once its removed. Cleanup
			//
			// if remoteURL, err := s.Workspace.GetRemoteURL(repoPath); err == nil {
			// 	if err := s.subs.subscribe(ctx, remoteURL, request.SessionID, s.Base.Logger, func(sessionID string) {
			// 		s.deleteSessionBySessionID(sessionID)
			// 	}); err != nil {
			// 		s.Base.Logger.Warn("Failed to subscribe to remote events",
			// 			"error", err,
			// 			"remote_url", remoteURL,
			// 			"session_id", request.SessionID,
			// 		)
			// 	}

			// DO STUFF
			return nil
		},
	})

	deleteSessionJob := tasky.NewJob(JobDeleteSession, tasky.JobConfig[Jobs]{
		Run: func(ctx context.Context, task *tasky.Task[Jobs]) error {
			ws := base.Workspace
			data := schemas.SessionDeleteRequest{}
			errs := schemas.SessionDeleteSchema.Parse(zjson.Decode(bytes.NewReader(task.Payload)), &data)
			if errs != nil {
				return fmt.Errorf("[queue] failed to validate payload: %s", zog.Issues.FlattenAndCollect(errs))
			}

			// TODO: get the path from the session ID from the DB somewhere. Right now we only have sessionId but we are storing {parent}#{sessionId} as folder name
			// worktreePath := path.Join(base.Config.Worktrees.Dir)
			worktreePath := base.Config.Worktrees.Dir

			commonGitDir, err := ws.GitCommonDirFromWorktree(worktreePath)
			if err != nil {
				return err
			}
			worktreeName := "" // TODO

			if err := ws.KillTmuxSession(worktreeName); err != nil {
				return err
			}
			if err := ws.RemoveGitWorktree(worktreePath); err != nil {
				return err
			}
			if err := ws.DeleteGitBranch(commonGitDir, data.SessionID); err != nil {
				return err
			}

			// Should be the last thing we do actually
			// remoteUrl, err := ws.GetRemoteURLFromWorktree(worktreePath)
			// if remoteURL, err := s.Base.Workspace.GetRemoteURLFromWorktree(worktreePath); err == nil {
			// 	if err := s.subs.unsubscribe(ctx, remoteURL, payload.SessionID, s.Base.Logger); err != nil {
			// 		s.Base.Logger.Warn("Failed to unsubscribe from remote events",
			// 			"error", err,
			// 			"remote_url", remoteURL,
			// 			"session_id", payload.SessionID,
			// 		)
			// 	}
			// }
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
				base.Logger.Error(fmt.Sprintf("Task %v from Job %v failed: %v", task.TaskID, task.JobID, err))
				return nil
			},
		},
	)

	assert.AssertNil(err, "[QUEUE] Failed to initialize")
	return q, nil
}
