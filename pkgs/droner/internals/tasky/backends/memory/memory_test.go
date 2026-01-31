package memory

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Oudwins/droner/pkgs/droner/internals/tasky"
)

func TestEnqueueDequeue(t *testing.T) {
	backend := New[string](Config{})
	jobID := "alpha"

	err := backend.Enqueue(context.Background(), tasky.Task[string]{
		JobID:   jobID,
		TaskID:  "t1",
		Payload: []byte("payload"),
	}, tasky.Job[string]{ID: jobID, Priority: 1})
	if err != nil {
		t.Fatalf("enqueue error: %v", err)
	}

	gotJob, gotTask, gotPayload, err := backend.Dequeue(context.Background())
	if err != nil {
		t.Fatalf("dequeue error: %v", err)
	}
	if gotJob != jobID || gotTask != "t1" || string(gotPayload) != "payload" {
		t.Fatalf("unexpected dequeue result: %v %v %s", gotJob, gotTask, string(gotPayload))
	}
}

func TestPriorityOrdering(t *testing.T) {
	backend := New[string](Config{})
	jobID := "alpha"

	_ = backend.Enqueue(context.Background(), tasky.Task[string]{
		JobID:   jobID,
		TaskID:  "low",
		Payload: []byte("low"),
	}, tasky.Job[string]{ID: jobID, Priority: 1})
	_ = backend.Enqueue(context.Background(), tasky.Task[string]{
		JobID:   jobID,
		TaskID:  "high",
		Payload: []byte("high"),
	}, tasky.Job[string]{ID: jobID, Priority: 10})

	_, first, _, _ := backend.Dequeue(context.Background())
	_, second, _, _ := backend.Dequeue(context.Background())

	if first != "high" || second != "low" {
		t.Fatalf("expected high then low, got %v then %v", first, second)
	}
}

func TestAckNackRetry(t *testing.T) {
	backend := New[string](Config{RetryMax: 1})
	jobID := "alpha"

	_ = backend.Enqueue(context.Background(), tasky.Task[string]{
		JobID:   jobID,
		TaskID:  "t1",
		Payload: []byte("payload"),
	}, tasky.Job[string]{ID: jobID, Priority: 1})

	_, taskID, _, _ := backend.Dequeue(context.Background())
	if err := backend.Nack(context.Background(), taskID); err != nil {
		t.Fatalf("nack error: %v", err)
	}

	_, taskID, _, _ = backend.Dequeue(context.Background())
	if err := backend.Nack(context.Background(), taskID); !errors.Is(err, ErrRetriesExceeded) {
		t.Fatalf("expected retries exceeded, got %v", err)
	}
}

func TestRetryDelayFunction(t *testing.T) {
	backend := New[string](Config{
		RetryMax:   2,
		RetryDelay: func(attempts int) time.Duration { return 50 * time.Millisecond },
	})
	jobID := "alpha"

	_ = backend.Enqueue(context.Background(), tasky.Task[string]{
		JobID:   jobID,
		TaskID:  "t1",
		Payload: []byte("payload"),
	}, tasky.Job[string]{ID: jobID, Priority: 1})

	_, taskID, _, _ := backend.Dequeue(context.Background())
	if err := backend.Nack(context.Background(), taskID); err != nil {
		t.Fatalf("nack error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, _, _, err := backend.Dequeue(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded before delay, got %v", err)
	}

	ctx, cancel = context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, gotTask, _, err := backend.Dequeue(ctx)
	if err != nil {
		t.Fatalf("unexpected dequeue error: %v", err)
	}
	if gotTask != "t1" {
		t.Fatalf("expected task t1, got %v", gotTask)
	}
}

func TestBatchingAndForceFlush(t *testing.T) {
	backend := New[string](Config{
		BatchMaxSize: 2,
		BatchMaxWait: 200 * time.Millisecond,
	})
	jobID := "alpha"

	_ = backend.Enqueue(context.Background(), tasky.Task[string]{
		JobID:   jobID,
		TaskID:  "t1",
		Payload: []byte("payload"),
	}, tasky.Job[string]{ID: jobID, Priority: 1})

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, _, _, err := backend.Dequeue(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded before batch flush, got %v", err)
	}

	_ = backend.Enqueue(context.Background(), tasky.Task[string]{
		JobID:   jobID,
		TaskID:  "t2",
		Payload: []byte("payload"),
	}, tasky.Job[string]{ID: jobID, Priority: 1})

	_, gotTask, _, err := backend.Dequeue(context.Background())
	if err != nil {
		t.Fatalf("dequeue error: %v", err)
	}
	if gotTask != "t1" && gotTask != "t2" {
		t.Fatalf("unexpected task id: %v", gotTask)
	}

	backend = New[string](Config{BatchMaxSize: 10, BatchMaxWait: time.Second})
	_ = backend.Enqueue(context.Background(), tasky.Task[string]{
		JobID:   jobID,
		TaskID:  "t3",
		Payload: []byte("payload"),
	}, tasky.Job[string]{ID: jobID, Priority: 1})
	if err := backend.ForceFlush(context.Background()); err != nil {
		t.Fatalf("force flush error: %v", err)
	}
	_, gotTask, _, err = backend.Dequeue(context.Background())
	if err != nil {
		t.Fatalf("dequeue error: %v", err)
	}
	if gotTask != "t3" {
		t.Fatalf("expected t3 after force flush, got %v", gotTask)
	}
}
