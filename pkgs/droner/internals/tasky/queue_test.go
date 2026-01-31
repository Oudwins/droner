package tasky

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type stubBackend[T ~string] struct {
	enqueue func(ctx context.Context, task Task[T]) error
	dequeue func(ctx context.Context) (JobID[T], TaskID, []byte, error)
	ack     func(ctx context.Context, taskID TaskID) error
	nack    func(ctx context.Context, taskID TaskID) error
	flush   func(ctx context.Context) error
}

func (b *stubBackend[T]) Enqueue(ctx context.Context, task Task[T]) error {
	if b.enqueue != nil {
		return b.enqueue(ctx, task)
	}
	return nil
}

func (b *stubBackend[T]) Dequeue(ctx context.Context) (JobID[T], TaskID, []byte, error) {
	if b.dequeue != nil {
		return b.dequeue(ctx)
	}
	var zero JobID[T]
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
	jobID := NewJobID("alpha")
	_, err = NewQueue(QueueConfig[string]{
		Backend: backend,
		Jobs: []Job[string]{
			{ID: jobID, Run: func(ctx context.Context, payload []byte) error { return nil }},
			{ID: jobID, Run: func(ctx context.Context, payload []byte) error { return nil }},
		},
	})
	if err == nil {
		t.Fatal("expected error for duplicate job id")
	}

	_, err = NewQueue(QueueConfig[string]{
		Backend: backend,
		Jobs: []Job[string]{
			{ID: NewJobID("beta")},
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
			{ID: NewJobID("alpha"), Run: func(ctx context.Context, payload []byte) error { return nil }},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = queue.Enqueue(context.Background(), Task[string]{
		JobID: NewJobID("missing"),
	})
	if err == nil {
		t.Fatal("expected error for unknown job id")
	}

	_, err = queue.Enqueue(context.Background(), Task[string]{
		JobID:  NewJobID("alpha"),
		TaskID: map[string]int{"bad": 1},
	})
	if err == nil {
		t.Fatal("expected error for non-comparable task id")
	}
}

func TestEnqueueGeneratesTaskIDAndPriority(t *testing.T) {
	backend := &stubBackend[string]{}
	var gotTask Task[string]
	backend.enqueue = func(ctx context.Context, task Task[string]) error {
		gotTask = task
		return nil
	}

	queue, err := NewQueue(QueueConfig[string]{
		Backend: backend,
		Jobs: []Job[string]{
			{ID: NewJobID("alpha"), Priority: 7, Run: func(ctx context.Context, payload []byte) error { return nil }},
		},
		TaskIDGen: TaskIDGeneratorFunc[string](func(jobID string) TaskID {
			return "gen-1"
		}),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	taskID, err := queue.Enqueue(context.Background(), Task[string]{
		JobID:   NewJobID("alpha"),
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
	if gotTask.Priority != 7 {
		t.Fatalf("expected priority 7, got %d", gotTask.Priority)
	}
}

func TestConsumerRunAckNack(t *testing.T) {
	dequeueCh := make(chan struct {
		jobID   JobID[string]
		taskID  TaskID
		payload []byte
		err     error
	})
	ackCh := make(chan TaskID, 1)
	nackCh := make(chan TaskID, 1)

	backend := &stubBackend[string]{
		dequeue: func(ctx context.Context) (JobID[string], TaskID, []byte, error) {
			select {
			case <-ctx.Done():
				var zero JobID[string]
				return zero, nil, nil, ctx.Err()
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
			{ID: NewJobID("ok"), Run: func(ctx context.Context, payload []byte) error { return nil }},
			{ID: NewJobID("fail"), Run: func(ctx context.Context, payload []byte) error { return errors.New("boom") }},
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
		jobID   JobID[string]
		taskID  TaskID
		payload []byte
		err     error
	}{jobID: NewJobID("ok"), taskID: "t1", payload: []byte("ok")}
	dequeueCh <- struct {
		jobID   JobID[string]
		taskID  TaskID
		payload []byte
		err     error
	}{jobID: NewJobID("fail"), taskID: "t2", payload: []byte("bad")}

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
	backend.dequeue = func(ctx context.Context) (JobID[string], TaskID, []byte, error) {
		return NewJobID("fail"), "t1", []byte("payload"), nil
	}
	backend.nack = func(ctx context.Context, taskID TaskID) error {
		return nil
	}

	stopErr := errors.New("stop")
	queue, err := NewQueue(QueueConfig[string]{
		Backend: backend,
		Jobs: []Job[string]{
			{ID: NewJobID("fail"), Run: func(ctx context.Context, payload []byte) error { return errors.New("boom") }},
		},
		OnError: func(err error, task Task[string], taskID TaskID, payload []byte) error {
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
		dequeue: func(ctx context.Context) (JobID[string], TaskID, []byte, error) {
			calls++
			return JobID[string]{}, nil, nil, context.Canceled
		},
	}
	queue, err := NewQueue(QueueConfig[string]{
		Backend: backend,
		Jobs: []Job[string]{
			{ID: NewJobID("ok"), Run: func(ctx context.Context, payload []byte) error { return nil }},
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

type TaskIDGeneratorFunc[T ~string] func(jobID T) TaskID

func (f TaskIDGeneratorFunc[T]) Next(jobID T) TaskID {
	return f(jobID)
}

func TestEnqueueConcurrentSafety(t *testing.T) {
	backend := &stubBackend[string]{}
	backend.enqueue = func(ctx context.Context, task Task[string]) error {
		return nil
	}

	queue, err := NewQueue(QueueConfig[string]{
		Backend: backend,
		Jobs: []Job[string]{
			{ID: NewJobID("alpha"), Run: func(ctx context.Context, payload []byte) error { return nil }},
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
			_, _ = queue.Enqueue(context.Background(), Task[string]{
				JobID:   NewJobID("alpha"),
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
