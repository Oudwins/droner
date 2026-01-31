package tasks

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/Oudwins/droner/pkgs/droner/internals/assert"
	"github.com/Oudwins/droner/pkgs/droner/internals/tasky"
)

type Jobs string

const (
	JobCreateSession Jobs = "session_delete_job"
	JobDeleteSession Jobs = "session_create_job"
)

func NewQueue() (*tasky.Queue[Jobs], error) {

	createSessionJob := tasky.NewJob(JobCreateSession, tasky.JobConfig{
		Run: func(ctx context.Context, payload []byte) error {
			// DO STUFF
			return nil
		},
	})

	deleteSessionJob := tasky.NewJob(JobDeleteSession, tasky.JobConfig{
		Run: func(ctx context.Context, payload []byte) error {
			// DO STUFF
			return nil
		},
	})

	q, err := tasky.NewQueue(
		tasky.QueueConfig[Jobs]{
			Jobs: []tasky.Job[Jobs]{createSessionJob, deleteSessionJob},
			OnError: func(err error, task *tasky.Task[Jobs], payload []byte) error {
				slog.Error(fmt.Sprintf("Task %v from Job %v failed: %v", task.TaskID, task.JobID, err))
				return nil
			},
		},
	)

	assert.AssertNil(err, "[QUEUE] Failed to initialize")
	return q, nil
}
