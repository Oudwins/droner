package tasky

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/Oudwins/droner/pkgs/droner/internals/tasky/generators/simple"
)

type Queue[T JobID] struct {
	jobs      map[T]*Job[T]
	backend   Backend[T]
	taskIDGen TaskIDGenerator[T]
	onError   OnErrorHandler[T]
}

type ConsumerOptions struct {
	Workers int
}

type Consumer[T JobID] struct {
	queue   *Queue[T]
	options ConsumerOptions
}

func NewQueue[T JobID](cfg QueueConfig[T]) (*Queue[T], error) {
	if cfg.Backend == nil {
		return nil, errors.New("backend is required")
	}

	jobs := make(map[T]*Job[T], len(cfg.Jobs))
	for i := range cfg.Jobs {
		job := &cfg.Jobs[i]
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

func (q *Queue[T]) Enqueue(ctx context.Context, task *Task[T]) (TaskID, error) {
	job, exists := q.jobs[task.JobID]
	if !exists {
		return "", fmt.Errorf("unknown job id: %v", task.JobID)
	}
	taskID := task.TaskID
	if taskID == "" {
		taskID = q.taskIDGen.Next(task.JobID)
		if taskID == "" {
			return "", errors.New("task id generator returned empty")
		}
	}
	//
	task.TaskID = taskID
	if err := q.backend.Enqueue(ctx, task, job); err != nil {
		return "", err
	}
	return taskID, nil
}

func NewConsumer[T JobID](queue *Queue[T], options ConsumerOptions) *Consumer[T] {
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
	reportError := func(err error, task *Task[T], payload []byte) {
		if err == nil {
			return
		}
		if c.queue.onError != nil {
			if onErr := c.queue.onError(err, task, payload); onErr != nil {
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
					reportError(err, nil, nil)
					continue
				}

				job, ok := c.queue.jobs[jobID]
				task := &Task[T]{
					JobID:   jobID,
					TaskID:  taskID,
					Payload: payload,
				}
				if !ok {
					reportError(fmt.Errorf("unknown job id: %v", jobID), task, payload)
					if ackErr := c.queue.backend.Ack(ctx, taskID); ackErr != nil {
						reportError(ackErr, task, payload)
					}
					continue
				}

				if err := job.Run(ctx, task); err != nil {
					if nackErr := c.queue.backend.Nack(ctx, taskID); nackErr != nil {
						reportError(nackErr, task, payload)
					} else {
						reportError(err, task, payload)
					}
					continue
				}

				if err := c.queue.backend.Ack(ctx, taskID); err != nil {
					reportError(err, task, payload)
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
