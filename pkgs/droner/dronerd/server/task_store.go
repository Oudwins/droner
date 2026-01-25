package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Oudwins/droner/pkgs/droner/internals/schemas"
	_ "modernc.org/sqlite"
)

type taskStore struct {
	db *sql.DB
}

type taskRecord struct {
	ID          string
	Type        string
	Status      schemas.TaskStatus
	CreatedAt   string
	StartedAt   string
	FinishedAt  string
	Error       string
	PayloadJSON string
	ResultJSON  string
}

func newTaskStore(dbPath string) (*taskStore, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		return nil, err
	}
	store := &taskStore{db: db}
	if err := store.init(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *taskStore) init() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS tasks (
	id TEXT PRIMARY KEY,
	type TEXT NOT NULL,
	status TEXT NOT NULL,
	created_at TEXT NOT NULL,
	started_at TEXT,
	finished_at TEXT,
	error TEXT,
	payload_json TEXT,
	result_json TEXT
);
CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status);
CREATE INDEX IF NOT EXISTS idx_tasks_created_at ON tasks(created_at);
`)
	return err
}

func (s *taskStore) create(ctx context.Context, record taskRecord) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO tasks (id, type, status, created_at, started_at, finished_at, error, payload_json, result_json)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
`, record.ID, record.Type, record.Status, record.CreatedAt, nullIfEmpty(record.StartedAt), nullIfEmpty(record.FinishedAt), nullIfEmpty(record.Error), nullIfEmpty(record.PayloadJSON), nullIfEmpty(record.ResultJSON))
	return err
}

func (s *taskStore) update(ctx context.Context, record taskRecord) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE tasks
SET status = ?, started_at = ?, finished_at = ?, error = ?, result_json = ?
WHERE id = ?
`, record.Status, nullIfEmpty(record.StartedAt), nullIfEmpty(record.FinishedAt), nullIfEmpty(record.Error), nullIfEmpty(record.ResultJSON), record.ID)
	return err
}

func (s *taskStore) get(ctx context.Context, id string) (*taskRecord, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, type, status, created_at, started_at, finished_at, error, payload_json, result_json
FROM tasks
WHERE id = ?
`, id)

	var record taskRecord
	var status string
	var startedAt sql.NullString
	var finishedAt sql.NullString
	var errMsg sql.NullString
	var payloadJSON sql.NullString
	var resultJSON sql.NullString
	if err := row.Scan(&record.ID, &record.Type, &status, &record.CreatedAt, &startedAt, &finishedAt, &errMsg, &payloadJSON, &resultJSON); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		return nil, err
	}
	record.Status = schemas.TaskStatus(status)
	record.StartedAt = startedAt.String
	record.FinishedAt = finishedAt.String
	record.Error = errMsg.String
	record.PayloadJSON = payloadJSON.String
	record.ResultJSON = resultJSON.String
	return &record, nil
}

func (s *taskStore) newRecord(taskID string, taskType string, payload any) (taskRecord, error) {
	createdAt := time.Now().UTC().Format(time.RFC3339Nano)
	var payloadJSON string
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return taskRecord{}, fmt.Errorf("failed to encode task payload: %w", err)
		}
		payloadJSON = string(data)
	}
	return taskRecord{
		ID:          taskID,
		Type:        taskType,
		Status:      schemas.TaskStatusPending,
		CreatedAt:   createdAt,
		PayloadJSON: payloadJSON,
	}, nil
}

func (s *taskStore) marshalResult(result any) (string, error) {
	if result == nil {
		return "", nil
	}
	data, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func nullIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}
