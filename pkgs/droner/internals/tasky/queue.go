package tasky

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"

	"github.com/Oudwins/droner/pkgs/droner/internals/tasky/generators/simple"
)

type Queue[T ~string] struct {
	jobs      map[JobID[T]]Job[T]
	backend   Backend[T]
	taskIDGen TaskIDGenerator[T]
	onError   OnErrorHandler[T]
}

type ConsumerOptions struct {
	Workers int
}

type Consumer[T ~string] struct {
	queue   *Queue[T]
	options ConsumerOptions
}

func CreateQueue[T ~string](cfg QueueConfig[T]) (*Queue[T], error) {
	if cfg.Backend == nil {
		return nil, errors.New("backend is required")
	}

	jobs := make(map[JobID[T]]Job[T], len(cfg.Jobs))
	for _, job := range cfg.Jobs {
		if _, exists := jobs[job.ID]; exists {
			return nil, fmt.Errorf("duplicate job id: %v", job.ID)
		}
		if job.Run == nil {
			return nil, fmt.Errorf("job %v has nil Run handler", job.ID)
		}
		jobs[job.ID] = job
	}

	taskIDGen := cfg.TaskIDGen
	if taskIDGen == nil {
		taskIDGen = simple.New[T]()
	}

	return &Queue[T]{
		jobs:      jobs,
		backend:   cfg.Backend,
		taskIDGen: taskIDGen,
		onError:   cfg.OnError,
	}, nil
}

func (q *Queue[T]) Enqueue(ctx context.Context, task Task[T]) (TaskID, error) {
	job, exists := q.jobs[task.JobID]
	if !exists {
		return nil, fmt.Errorf("unknown job id: %v", task.JobID)
	}

	taskID := task.TaskID
	if taskID == nil {
		taskID = q.taskIDGen.Next(task.JobID.Value)
		if taskID == nil {
			return nil, errors.New("task id generator returned nil")
		}
	}

	taskType := reflect.TypeOf(taskID)
	if taskType == nil || !taskType.Comparable() {
		return nil, fmt.Errorf("task id must be comparable: %T", taskID)
	}

	task.Priority = job.Priority
	task.TaskID = taskID
	task.Priority = job.Priority
	if err := q.backend.Enqueue(ctx, task); err != nil {
		return nil, err
	}

	return taskID, nil
}

func NewConsumer[T ~string](queue *Queue[T], options ConsumerOptions) *Consumer[T] {
	if options.Workers <= 0 {
		options.Workers = 1
	}
	return &Consumer[T]{
		queue:   queue,
		options: options,
	}
}

func (c *Consumer[T]) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var runErr atomic.Value
	var once sync.Once
	reportError := func(err error, task Task[T], taskID TaskID, payload []byte) {
		if err == nil {
			return
		}
		if c.queue.onError != nil {
			if onErr := c.queue.onError(err, task, taskID, payload); onErr != nil {
				once.Do(func() {
					runErr.Store(onErr)
					cancel()
				})
			}
		}
	}

	var wg sync.WaitGroup
	workerCount := c.options.Workers
	wg.Add(workerCount)

	for i := 0; i < workerCount; i++ {
		go func() {
			defer wg.Done()
			for {
				if ctx.Err() != nil {
					return
				}

				jobID, taskID, payload, err := c.queue.backend.Dequeue(ctx)
				if err != nil {
					if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
						return
					}
					reportError(err, Task[T]{}, nil, nil)
					continue
				}

				job, ok := c.queue.jobs[jobID]
				task := Task[T]{
					JobID:   jobID,
					TaskID:  taskID,
					Payload: payload,
				}
				if !ok {
					reportError(fmt.Errorf("unknown job id: %v", jobID), task, taskID, payload)
					if ackErr := c.queue.backend.Ack(ctx, taskID); ackErr != nil {
						reportError(ackErr, task, taskID, payload)
					}
					continue
				}

				if err := job.Run(ctx, payload); err != nil {
					if nackErr := c.queue.backend.Nack(ctx, taskID); nackErr != nil {
						reportError(nackErr, task, taskID, payload)
					} else {
						reportError(err, task, taskID, payload)
					}
					continue
				}

				if err := c.queue.backend.Ack(ctx, taskID); err != nil {
					reportError(err, task, taskID, payload)
				}
			}
		}()
	}

	wg.Wait()

	if errValue := runErr.Load(); errValue != nil {
		if err, ok := errValue.(error); ok {
			return err
		}
	}

	return nil
}
