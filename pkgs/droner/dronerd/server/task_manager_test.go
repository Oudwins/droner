package server

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/Oudwins/droner/pkgs/droner/internals/schemas"
	"github.com/Oudwins/droner/pkgs/droner/internals/testutil"
)

func waitForStatus(store *taskStore, taskID string, status schemas.TaskStatus) error {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		record, err := store.get(context.Background(), taskID)
		if err == nil && record.Status == status {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return errors.New("timeout waiting for status")
}

func TestTaskManagerEnqueueLifecycleSuccess(t *testing.T) {
	store, err := newTaskStore(testutil.TempDBPath(t))
	if err != nil {
		t.Fatalf("newTaskStore: %v", err)
	}
	manager := newTaskManager(store, slog.New(slog.NewJSONHandler(io.Discard, nil)))

	release := make(chan struct{})
	resp, err := manager.Enqueue("task", map[string]string{"a": "b"}, func(ctx context.Context) (any, error) {
		<-release
		return &schemas.TaskResult{SessionID: "ok"}, nil
	})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if resp.Status != schemas.TaskStatusPending {
		t.Fatalf("expected pending, got %s", resp.Status)
	}

	if err := waitForStatus(store, resp.TaskID, schemas.TaskStatusRunning); err != nil {
		t.Fatalf("wait for running: %v", err)
	}

	close(release)
	if err := waitForStatus(store, resp.TaskID, schemas.TaskStatusSucceeded); err != nil {
		t.Fatalf("wait for succeeded: %v", err)
	}

	final, err := manager.Get(resp.TaskID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if final.Result == nil || final.Result.SessionID != "ok" {
		t.Fatalf("unexpected result: %+v", final.Result)
	}
}

func TestTaskManagerEnqueueLifecycleFailure(t *testing.T) {
	store, err := newTaskStore(testutil.TempDBPath(t))
	if err != nil {
		t.Fatalf("newTaskStore: %v", err)
	}
	manager := newTaskManager(store, slog.New(slog.NewJSONHandler(io.Discard, nil)))

	resp, err := manager.Enqueue("task", nil, func(ctx context.Context) (any, error) {
		return nil, errors.New("boom")
	})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	if err := waitForStatus(store, resp.TaskID, schemas.TaskStatusFailed); err != nil {
		t.Fatalf("wait for failed: %v", err)
	}

	final, err := manager.Get(resp.TaskID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if final.Status != schemas.TaskStatusFailed {
		t.Fatalf("expected failed, got %s", final.Status)
	}
	if final.Error == "" {
		t.Fatalf("expected error to be set")
	}
}
