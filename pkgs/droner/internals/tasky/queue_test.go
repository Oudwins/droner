package tasky

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type stubBackend[T JobID] struct {
	enqueue func(ctx context.Context, task *Task[T], job *Job[T]) error
	dequeue func(ctx context.Context) (T, TaskID, []byte, error)
	ack     func(ctx context.Context, taskID TaskID) error
	nack    func(ctx context.Context, taskID TaskID) error
	flush   func(ctx context.Context) error
}

func (b *stubBackend[T]) Enqueue(ctx context.Context, task *Task[T], job *Job[T]) error {
	if b.enqueue != nil {
		return b.enqueue(ctx, task, job)
	}
	return nil
}

func (b *stubBackend[T]) Dequeue(ctx context.Context) (T, TaskID, []byte, error) {
	if b.dequeue != nil {
		return b.dequeue(ctx)
	}
	var zero T
	return zero, nil, nil, context.Canceled
}

func (b *stubBackend[T]) Ack(ctx context.Context, taskID TaskID) error {
	if b.ack != nil {
		return b.ack(ctx, taskID)
	}
	return nil
}

func (b *stubBackend[T]) Nack(ctx context.Context, taskID TaskID) error {
	if b.nack != nil {
		return b.nack(ctx, taskID)
	}
	return nil
}

func (b *stubBackend[T]) ForceFlush(ctx context.Context) error {
	if b.flush != nil {
		return b.flush(ctx)
	}
	return nil
}

func TestNewQueueValidation(t *testing.T) {
	_, err := NewQueue(QueueConfig[string]{})
	if err == nil {
		t.Fatal("expected error for nil backend")
	}

	backend := &stubBackend[string]{}
	jobID := "alpha"
	_, err = NewQueue(QueueConfig[string]{
		Backend: backend,
		Jobs: []Job[string]{
			NewJob(jobID, JobConfig[string]{Run: func(ctx context.Context, task *Task[string]) error { return nil }}),
			NewJob(jobID, JobConfig[string]{Run: func(ctx context.Context, task *Task[string]) error { return nil }}),
		},
	})
	if err == nil {
		t.Fatal("expected error for duplicate job id")
	}

	_, err = NewQueue(QueueConfig[string]{
		Backend: backend,
		Jobs: []Job[string]{
			{ID: "beta"},
		},
	})
	if err == nil {
		t.Fatal("expected error for nil Run handler")
	}
}

func TestEnqueueValidation(t *testing.T) {
	backend := &stubBackend[string]{}
	queue, err := NewQueue(QueueConfig[string]{
		Backend: backend,
		Jobs: []Job[string]{
			NewJob("alpha", JobConfig[string]{Run: func(ctx context.Context, task *Task[string]) error { return nil }}),
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = queue.Enqueue(context.Background(), &Task[string]{
		JobID: "missing",
	})
	if err == nil {
		t.Fatal("expected error for unknown job id")
	}

	_, err = queue.Enqueue(context.Background(), &Task[string]{
		JobID:  "alpha",
		TaskID: map[string]int{"bad": 1},
	})
	if err == nil {
		t.Fatal("expected error for non-comparable task id")
	}
}

func TestEnqueueGeneratesTaskIDAndPriority(t *testing.T) {
	backend := &stubBackend[string]{}
	var gotTask *Task[string]
	var gotPriority int
	backend.enqueue = func(ctx context.Context, task *Task[string], job *Job[string]) error {
		gotTask = task
		gotPriority = job.Priority
		return nil
	}

	queue, err := NewQueue(QueueConfig[string]{
		Backend: backend,
		Jobs: []Job[string]{
			NewJob("alpha", JobConfig[string]{Priority: 7, Run: func(ctx context.Context, task *Task[string]) error { return nil }}),
		},
		TaskIDGen: TaskIDGeneratorFunc[string](func(jobID string) TaskID {
			return "gen-1"
		}),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	taskID, err := queue.Enqueue(context.Background(), &Task[string]{
		JobID:   "alpha",
		Payload: []byte("payload"),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if taskID != "gen-1" {
		t.Fatalf("expected task id gen-1, got %v", taskID)
	}
	if gotTask.TaskID != "gen-1" {
		t.Fatalf("expected backend task id gen-1, got %v", gotTask.TaskID)
	}
	if gotPriority != 7 {
		t.Fatalf("expected priority 7, got %d", gotPriority)
	}
}

func TestConsumerRunAckNack(t *testing.T) {
	dequeueCh := make(chan struct {
		jobID   string
		taskID  TaskID
		payload []byte
		err     error
	})
	ackCh := make(chan TaskID, 1)
	nackCh := make(chan TaskID, 1)

	backend := &stubBackend[string]{
		dequeue: func(ctx context.Context) (string, TaskID, []byte, error) {
			select {
			case <-ctx.Done():
				return "", nil, nil, ctx.Err()
			case item := <-dequeueCh:
				return item.jobID, item.taskID, item.payload, item.err
			}
		},
		ack: func(ctx context.Context, taskID TaskID) error {
			ackCh <- taskID
			return nil
		},
		nack: func(ctx context.Context, taskID TaskID) error {
			nackCh <- taskID
			return nil
		},
	}

	queue, err := NewQueue(QueueConfig[string]{
		Backend: backend,
		Jobs: []Job[string]{
			NewJob("ok", JobConfig[string]{Run: func(ctx context.Context, task *Task[string]) error { return nil }}),
			NewJob("fail", JobConfig[string]{Run: func(ctx context.Context, task *Task[string]) error { return errors.New("boom") }}),
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	consumer := NewConsumer(queue, ConsumerOptions{Workers: 1})
	done := make(chan error, 1)
	go func() {
		done <- consumer.Run(ctx)
	}()

	dequeueCh <- struct {
		jobID   string
		taskID  TaskID
		payload []byte
		err     error
	}{jobID: "ok", taskID: "t1", payload: []byte("ok")}
	dequeueCh <- struct {
		jobID   string
		taskID  TaskID
		payload []byte
		err     error
	}{jobID: "fail", taskID: "t2", payload: []byte("bad")}

	select {
	case got := <-ackCh:
		if got != "t1" {
			t.Fatalf("expected ack for t1, got %v", got)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for ack")
	}

	select {
	case got := <-nackCh:
		if got != "t2" {
			t.Fatalf("expected nack for t2, got %v", got)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for nack")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for consumer shutdown")
	}
}

func TestConsumerOnErrorStops(t *testing.T) {
	backend := &stubBackend[string]{}
	backend.dequeue = func(ctx context.Context) (string, TaskID, []byte, error) {
		return "fail", "t1", []byte("payload"), nil
	}
	backend.nack = func(ctx context.Context, taskID TaskID) error {
		return nil
	}

	stopErr := errors.New("stop")
	queue, err := NewQueue(QueueConfig[string]{
		Backend: backend,
		Jobs: []Job[string]{
			NewJob("fail", JobConfig[string]{Run: func(ctx context.Context, task *Task[string]) error { return errors.New("boom") }}),
		},
		OnError: func(err error, task *Task[string], payload []byte) error {
			return stopErr
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	consumer := NewConsumer(queue, ConsumerOptions{Workers: 1})
	if err := consumer.Run(context.Background()); !errors.Is(err, stopErr) {
		t.Fatalf("expected stop error, got %v", err)
	}
}

func TestConsumerDefaultsWorkers(t *testing.T) {
	var calls int
	backend := &stubBackend[string]{
		dequeue: func(ctx context.Context) (string, TaskID, []byte, error) {
			calls++
			return "", nil, nil, context.Canceled
		},
	}
	queue, err := NewQueue(QueueConfig[string]{
		Backend: backend,
		Jobs: []Job[string]{
			NewJob("ok", JobConfig[string]{Run: func(ctx context.Context, task *Task[string]) error { return nil }}),
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	consumer := NewConsumer(queue, ConsumerOptions{})
	_ = consumer.Run(context.Background())
	if calls != 1 {
		t.Fatalf("expected 1 dequeue call, got %d", calls)
	}
}

type TaskIDGeneratorFunc[T JobID] func(jobID T) TaskID

func (f TaskIDGeneratorFunc[T]) Next(jobID T) TaskID {
	return f(jobID)
}

func TestEnqueueConcurrentSafety(t *testing.T) {
	backend := &stubBackend[string]{}
	backend.enqueue = func(ctx context.Context, task *Task[string], job *Job[string]) error {
		return nil
	}

	queue, err := NewQueue(QueueConfig[string]{
		Backend: backend,
		Jobs: []Job[string]{
			NewJob("alpha", JobConfig[string]{Run: func(ctx context.Context, task *Task[string]) error { return nil }}),
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = queue.Enqueue(context.Background(), &Task[string]{
				JobID:   "alpha",
				Payload: []byte("data"),
			})
		}()
	}
	wt := make(chan struct{})
	go func() {
		wg.Wait()
		close(wt)
	}()
	select {
	case <-wt:
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for concurrent enqueue")
	}
}
