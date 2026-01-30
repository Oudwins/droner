package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"

	"github.com/Oudwins/droner/pkgs/droner/internals/schemas"
	"github.com/Oudwins/droner/pkgs/droner/internals/testutil"
)

func TestNewTaskStoreInitializesSchema(t *testing.T) {
	store, err := newTaskStore(testutil.TempDBPath(t))
	if err != nil {
		t.Fatalf("newTaskStore: %v", err)
	}

	row := store.db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='tasks'")
	var name string
	if err := row.Scan(&name); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if name != "tasks" {
		t.Fatalf("expected tasks table, got %q", name)
	}
}

func TestTaskStoreRecordRoundTrip(t *testing.T) {
	store, err := newTaskStore(testutil.TempDBPath(t))
	if err != nil {
		t.Fatalf("newTaskStore: %v", err)
	}

	record, err := store.newRecord("task1", "type", map[string]string{"foo": "bar"})
	if err != nil {
		t.Fatalf("newRecord: %v", err)
	}
	if err := store.create(context.Background(), record); err != nil {
		t.Fatalf("create: %v", err)
	}

	update := taskRecord{ID: "task1", Status: schemas.TaskStatusRunning, StartedAt: "now"}
	if err := store.update(context.Background(), update); err != nil {
		t.Fatalf("update: %v", err)
	}

	got, err := store.get(context.Background(), "task1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != schemas.TaskStatusRunning {
		t.Fatalf("expected status running, got %s", got.Status)
	}
	if got.StartedAt == "" {
		t.Fatalf("expected started_at to be set")
	}
}

func TestTaskStoreNewRecordPayload(t *testing.T) {
	store, err := newTaskStore(testutil.TempDBPath(t))
	if err != nil {
		t.Fatalf("newTaskStore: %v", err)
	}

	record, err := store.newRecord("task1", "type", map[string]string{"foo": "bar"})
	if err != nil {
		t.Fatalf("newRecord: %v", err)
	}
	if record.PayloadJSON == "" {
		t.Fatalf("expected payload json")
	}

	var decoded map[string]string
	if err := json.Unmarshal([]byte(record.PayloadJSON), &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded["foo"] != "bar" {
		t.Fatalf("unexpected payload %v", decoded)
	}
}

func TestTaskStoreMarshalResultNil(t *testing.T) {
	store, err := newTaskStore(testutil.TempDBPath(t))
	if err != nil {
		t.Fatalf("newTaskStore: %v", err)
	}
	result, err := store.marshalResult(nil)
	if err != nil {
		t.Fatalf("marshalResult: %v", err)
	}
	if result != "" {
		t.Fatalf("expected empty result, got %q", result)
	}
}

func TestTaskManagerGetDecodesResult(t *testing.T) {
	store, err := newTaskStore(testutil.TempDBPath(t))
	if err != nil {
		t.Fatalf("newTaskStore: %v", err)
	}
	manager := newTaskManager(store, slog.New(slog.NewJSONHandler(io.Discard, nil)))

	resp, err := manager.Enqueue("task", nil, func(ctx context.Context) (any, error) {
		return &schemas.TaskResult{SessionID: "abc"}, nil
	})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	if err := waitForStatus(store, resp.TaskID, schemas.TaskStatusSucceeded); err != nil {
		t.Fatalf("wait for success: %v", err)
	}

	final, err := manager.Get(resp.TaskID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if final.Result == nil || final.Result.SessionID != "abc" {
		t.Fatalf("expected decoded result, got %+v", final.Result)
	}
}
