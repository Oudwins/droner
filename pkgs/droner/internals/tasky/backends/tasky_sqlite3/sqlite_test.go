package taskysqlite3

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Oudwins/droner/pkgs/droner/internals/tasky"
	_ "modernc.org/sqlite"
)

func setupSQLiteBackend(t *testing.T, cfg Config) (*Backend[string], *sql.DB) {
	t.Helper()

	if cfg.DB == nil {
		dbDir := filepath.Join(".temp", "tasky")
		if err := os.MkdirAll(dbDir, 0o755); err != nil {
			t.Fatalf("db dir error: %v", err)
		}
		name := strings.NewReplacer("/", "_", "\\", "_").Replace(t.Name())
		path := filepath.Join(dbDir, name+"_"+time.Now().UTC().Format("20060102150405.000000000")+".db")
		db, err := sql.Open("sqlite", path)
		if err != nil {
			t.Fatalf("db open error: %v", err)
		}
		cfg.DB = db
		t.Cleanup(func() {
			_ = db.Close()
			_ = os.Remove(path)
		})
	}

	backend, err := New[string](cfg)
	if err != nil {
		t.Fatalf("backend init error: %v", err)
	}

	return backend, cfg.DB
}

func TestEnqueueDequeueSQLite(t *testing.T) {
	backend, _ := setupSQLiteBackend(t, Config{QueueName: "queue_basic"})

	jobID := "alpha"
	if err := backend.Enqueue(context.Background(), &tasky.Task[string]{
		JobID:   jobID,
		TaskID:  "t1",
		Payload: []byte("payload"),
	}, &tasky.Job[string]{ID: jobID, Priority: 1}); err != nil {
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

func TestAckMarksCompleted(t *testing.T) {
	backend, db := setupSQLiteBackend(t, Config{QueueName: "queue_ack"})

	jobID := "alpha"
	if err := backend.Enqueue(context.Background(), &tasky.Task[string]{
		JobID:   jobID,
		TaskID:  "t1",
		Payload: []byte("payload"),
	}, &tasky.Job[string]{ID: jobID, Priority: 1}); err != nil {
		t.Fatalf("enqueue error: %v", err)
	}

	_, taskID, _, err := backend.Dequeue(context.Background())
	if err != nil {
		t.Fatalf("dequeue error: %v", err)
	}
	if err := backend.Ack(context.Background(), taskID); err != nil {
		t.Fatalf("ack error: %v", err)
	}

	status, completedAt := queryStatus(t, db, "queue_ack", "t1")
	if status != "completed" {
		t.Fatalf("expected completed status, got %s", status)
	}
	if completedAt == 0 {
		t.Fatalf("expected completed_at to be set")
	}
}

func TestRetryFailedStatus(t *testing.T) {
	backend, db := setupSQLiteBackend(t, Config{QueueName: "queue_fail", RetryMax: 0})

	jobID := "alpha"
	if err := backend.Enqueue(context.Background(), &tasky.Task[string]{
		JobID:   jobID,
		TaskID:  "t1",
		Payload: []byte("payload"),
	}, &tasky.Job[string]{ID: jobID, Priority: 1}); err != nil {
		t.Fatalf("enqueue error: %v", err)
	}

	_, taskID, _, err := backend.Dequeue(context.Background())
	if err != nil {
		t.Fatalf("dequeue error: %v", err)
	}
	if err := backend.Nack(context.Background(), taskID); !errors.Is(err, ErrRetriesExceeded) {
		t.Fatalf("expected retries exceeded, got %v", err)
	}

	status, _ := queryStatus(t, db, "queue_fail", "t1")
	if status != "failed" {
		t.Fatalf("expected failed status, got %s", status)
	}
}

func TestRetryDelayAvailableAt(t *testing.T) {
	backend, db := setupSQLiteBackend(t, Config{
		QueueName:  "queue_delay",
		RetryMax:   1,
		RetryDelay: func(attempts int) time.Duration { return 200 * time.Millisecond },
	})

	jobID := "alpha"
	if err := backend.Enqueue(context.Background(), &tasky.Task[string]{
		JobID:   jobID,
		TaskID:  "t1",
		Payload: []byte("payload"),
	}, &tasky.Job[string]{ID: jobID, Priority: 1}); err != nil {
		t.Fatalf("enqueue error: %v", err)
	}

	_, taskID, _, err := backend.Dequeue(context.Background())
	if err != nil {
		t.Fatalf("dequeue error: %v", err)
	}
	if err := backend.Nack(context.Background(), taskID); err != nil {
		t.Fatalf("nack error: %v", err)
	}

	availableAt := queryAvailableAt(t, db, "queue_delay", "t1")
	if availableAt <= time.Now().UTC().UnixNano() {
		t.Fatalf("expected available_at in the future")
	}
}

func TestQueueNameValidation(t *testing.T) {
	_, err := New[string](Config{QueueName: "bad-name"})
	if err == nil {
		t.Fatal("expected error for invalid queue name")
	}

	backend, db := setupSQLiteBackend(t, Config{QueueName: "queue_custom"})

	jobID := "alpha"
	if err := backend.Enqueue(context.Background(), &tasky.Task[string]{
		JobID:   jobID,
		TaskID:  "t1",
		Payload: []byte("payload"),
	}, &tasky.Job[string]{ID: jobID, Priority: 1}); err != nil {
		t.Fatalf("enqueue error: %v", err)
	}

	var name string
	row := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='queue_custom'`)
	if err := row.Scan(&name); err != nil {
		t.Fatalf("expected custom queue table, got error: %v", err)
	}
}

func TestBatchingFlushes(t *testing.T) {
	backend, _ := setupSQLiteBackend(t, Config{QueueName: "queue_batch", BatchMaxSize: 2})

	jobID := "alpha"
	_ = backend.Enqueue(context.Background(), &tasky.Task[string]{
		JobID:   jobID,
		TaskID:  "t1",
		Payload: []byte("payload"),
	}, &tasky.Job[string]{ID: jobID, Priority: 1})
	_ = backend.Enqueue(context.Background(), &tasky.Task[string]{
		JobID:   jobID,
		TaskID:  "t2",
		Payload: []byte("payload"),
	}, &tasky.Job[string]{ID: jobID, Priority: 1})

	_, taskID, _, err := backend.Dequeue(context.Background())
	if err != nil {
		t.Fatalf("dequeue error: %v", err)
	}
	if taskID != "t1" && taskID != "t2" {
		t.Fatalf("unexpected task id: %v", taskID)
	}
}

func queryStatus(t *testing.T, db *sql.DB, queueName, taskID string) (string, int64) {
	t.Helper()
	row := db.QueryRow(`SELECT status, completed_at FROM `+queueName+` WHERE id = ?`, taskID)
	var status string
	var completedAt sql.NullInt64
	if err := row.Scan(&status, &completedAt); err != nil {
		t.Fatalf("query error: %v", err)
	}
	if completedAt.Valid {
		return status, completedAt.Int64
	}
	return status, 0
}

func queryAvailableAt(t *testing.T, db *sql.DB, queueName, taskID string) int64 {
	t.Helper()
	row := db.QueryRow(`SELECT available_at FROM `+queueName+` WHERE id = ?`, taskID)
	var availableAt int64
	if err := row.Scan(&availableAt); err != nil {
		t.Fatalf("query error: %v", err)
	}
	return availableAt
}
