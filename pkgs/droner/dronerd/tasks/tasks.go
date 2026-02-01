package tasks

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"path"
	"path/filepath"

	"github.com/Oudwins/droner/pkgs/droner/dronerd/baseserver"
	"github.com/Oudwins/droner/pkgs/droner/internals/assert"
	"github.com/Oudwins/droner/pkgs/droner/internals/schemas"
	"github.com/Oudwins/droner/pkgs/droner/internals/tasky"
	"github.com/Oudwins/droner/pkgs/droner/internals/tasky/backends/tasky_sqlite3"
	"github.com/Oudwins/zog/parsers/zjson"
)

type Jobs string

const (
	JobCreateSession Jobs = "session_delete_job"
	JobDeleteSession Jobs = "session_create_job"
)

// worktreeName := filepath.Base(worktreePath)
// if err := s.Workspace.CreateGitWorktree(repoPath, worktreePath, request.SessionID); err != nil {
// 	return nil, err
// }
//
// if err := s.Workspace.RunWorktreeSetup(repoPath, worktreePath); err != nil {
// 	return nil, err
// }
//
// if err := s.Workspace.CreateTmuxSession(worktreeName, worktreePath, request.Agent.Model, request.Agent.Prompt); err != nil {
// 	return nil, err
// }
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
// } else {
// 	s.Base.Logger.Warn("Failed to get remote URL, skipping event subscription",
// 		"error", err,
// 		"session_id", request.SessionID,
// 	)
// }

// return &schemas.TaskResult{SessionID: request.SessionID, WorktreePath: worktreePath}, nil

func NewQueue(base *baseserver.BaseServer) (*tasky.Queue[Jobs], error) {

	createSessionJob := tasky.NewJob(JobCreateSession, tasky.JobConfig[Jobs]{
		Run: func(ctx context.Context, task *tasky.Task[Jobs]) error {
			ws := base.Workspace
			payload := schemas.SessionCreateRequest{}
			schemas.SessionCreateSchema.Parse(zjson.Decode(bytes.NewReader(task.Payload)), &payload)

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

			// DO STUFF
			return nil
		},
	})

	deleteSessionJob := tasky.NewJob(JobDeleteSession, tasky.JobConfig[Jobs]{
		Run: func(ctx context.Context, task *tasky.Task[Jobs]) error {
			// DO STUFF
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
