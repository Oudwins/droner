package tasky

import (
	"context"
)

type JobID[T ~string] struct {
	Value T
}

func NewJobID[T ~string](value T) JobID[T] {
	return JobID[T]{Value: value}
}

type Job[T ~string] struct {
	ID       JobID[T]
	Priority int
	Run      func(ctx context.Context, payload []byte) error
}

type TaskID = any

type Task[T ~string] struct {
	JobID    JobID[T]
	TaskID   TaskID
	Payload  []byte
	Priority int
}

type TaskIDGenerator[T ~string] interface {
	Next(jobID T) TaskID
}

type OnErrorHandler[T ~string] func(err error, task Task[T], taskID TaskID, payload []byte) error

type QueueConfig[T ~string] struct {
	Jobs      []Job[T]
	Backend   Backend[T]
	TaskIDGen TaskIDGenerator[T]
	OnError   OnErrorHandler[T]
}

type Backend[T ~string] interface {
	Enqueue(ctx context.Context, task Task[T]) error
	Dequeue(ctx context.Context) (jobID JobID[T], taskID TaskID, payload []byte, err error)
	Ack(ctx context.Context, taskID TaskID) error
	Nack(ctx context.Context, taskID TaskID) error
	ForceFlush(ctx context.Context) error
}
