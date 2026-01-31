package tasky

import "context"

type JobID interface {
	~string
}

type Job[T JobID] struct {
	ID       T
	Priority int
	Run      func(ctx context.Context, payload []byte) error
}

type TaskID = any

type JobConfig struct {
	Priority int
	Run      func(ctx context.Context, payload []byte) error
}

type Task[T JobID] struct {
	JobID   T
	TaskID  TaskID
	Payload []byte
}

type TaskIDGenerator[T JobID] interface {
	Next(jobID T) TaskID
}

type OnErrorHandler[T JobID] func(err error, task *Task[T], taskID TaskID, payload []byte) error

type QueueConfig[T JobID] struct {
	Jobs      []Job[T]
	Backend   Backend[T]
	TaskIDGen TaskIDGenerator[T]
	OnError   OnErrorHandler[T]
}

type Backend[T JobID] interface {
	Enqueue(ctx context.Context, task *Task[T], job *Job[T]) error
	Dequeue(ctx context.Context) (jobID T, taskID TaskID, payload []byte, err error)
	Ack(ctx context.Context, taskID TaskID) error
	Nack(ctx context.Context, taskID TaskID) error
	ForceFlush(ctx context.Context) error
}

func NewJob[T JobID](id T, config JobConfig) Job[T] {
	return Job[T]{
		ID:       id,
		Priority: config.Priority,
		Run:      config.Run,
	}
}

func NewTask[T JobID](jobID T, payload []byte) Task[T] {
	return Task[T]{
		JobID:   jobID,
		Payload: payload,
	}
}

func NewQueueConfig[T JobID](jobs []Job[T], backend Backend[T]) QueueConfig[T] {
	return QueueConfig[T]{
		Jobs:    jobs,
		Backend: backend,
	}
}
