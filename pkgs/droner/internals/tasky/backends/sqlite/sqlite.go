package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"sync"
	"time"

	"github.com/Oudwins/droner/pkgs/droner/internals/tasky"
	_ "modernc.org/sqlite"
)

var ErrRetriesExceeded = errors.New("retries exceeded")

type Config struct {
	Path         string
	DB           *sql.DB
	QueueName    string
	BatchMaxSize int
	BatchMaxWait time.Duration
	RetryDelay   func(attempts int) time.Duration
	RetryMax     int
	PollInterval time.Duration
}

type Backend[T ~string] struct {
	mu         sync.Mutex
	db         *sql.DB
	batch      []queueItem[T]
	batchTimer *time.Timer
	signal     chan struct{}
	cfg        Config
}

func New[T ~string](cfg Config) (*Backend[T], error) {
	if cfg.DB == nil && cfg.Path == "" {
		return nil, errors.New("sqlite backend requires a db or path")
	}

	db := cfg.DB
	if db == nil {
		opened, err := sql.Open("sqlite", cfg.Path)
		if err != nil {
			return nil, err
		}
		if err := opened.Ping(); err != nil {
			return nil, err
		}
		db = opened
	}

	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 200 * time.Millisecond
	}
	if cfg.QueueName == "" {
		cfg.QueueName = "tasky_queue"
	}
	if err := validateQueueName(cfg.QueueName); err != nil {
		return nil, err
	}

	backend := &Backend[T]{
		db:     db,
		signal: make(chan struct{}, 1),
		cfg:    cfg,
	}

	if err := backend.init(); err != nil {
		return nil, err
	}

	return backend, nil
}

func (b *Backend[T]) Enqueue(ctx context.Context, task tasky.Task[T]) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	taskKey, err := taskIDKey(task.TaskID)
	if err != nil {
		return err
	}

	item := queueItem[T]{
		taskID:    taskKey,
		jobID:     string(task.JobID.Value),
		payload:   task.Payload,
		priority:  task.Priority,
		attempts:  0,
		createdAt: time.Now().UTC().UnixNano(),
		availAt:   time.Now().UTC().UnixNano(),
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.batchEnabledLocked() {
		b.batch = append(b.batch, item)
		if b.cfg.BatchMaxSize > 0 && len(b.batch) >= b.cfg.BatchMaxSize {
			return b.flushBatchLocked(ctx)
		}
		if b.cfg.BatchMaxWait > 0 && b.batchTimer == nil {
			b.batchTimer = time.AfterFunc(b.cfg.BatchMaxWait, b.flushBatchFromTimer)
		}
		return nil
	}

	if err := b.insertItem(ctx, item); err != nil {
		return err
	}

	b.signalLocked()
	return nil
}

func (b *Backend[T]) Dequeue(ctx context.Context) (tasky.JobID[T], tasky.TaskID, []byte, error) {
	for {
		if ctx.Err() != nil {
			var zero tasky.JobID[T]
			return zero, nil, nil, ctx.Err()
		}

		item, err := b.tryDequeue(ctx)
		if err != nil {
			return tasky.JobID[T]{}, nil, nil, err
		}
		if item != nil {
			jobID := tasky.JobID[T]{Value: T(item.jobID)}
			return jobID, item.taskID, item.payload, nil
		}

		select {
		case <-ctx.Done():
			var zero tasky.JobID[T]
			return zero, nil, nil, ctx.Err()
		case <-b.signal:
		case <-time.After(b.cfg.PollInterval):
		}
	}
}

func (b *Backend[T]) Ack(ctx context.Context, taskID tasky.TaskID) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	key, err := taskIDKey(taskID)
	if err != nil {
		return err
	}

	now := time.Now().UTC().UnixNano()
	res, err := b.db.ExecContext(ctx, fmt.Sprintf(`
UPDATE %s
SET status = 'completed', updated_at = ?, completed_at = ?
WHERE id = ?
`, b.cfg.QueueName), now, now, key)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("unknown task id: %v", taskID)
	}
	return nil
}

func (b *Backend[T]) Nack(ctx context.Context, taskID tasky.TaskID) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	key, err := taskIDKey(taskID)
	if err != nil {
		return err
	}

	return b.retryTask(ctx, key)
}

func (b *Backend[T]) ForceFlush(ctx context.Context) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.flushBatchLocked(ctx)
}

func (b *Backend[T]) Close() error {
	if b.cfg.DB != nil {
		return nil
	}
	return b.db.Close()
}

func (b *Backend[T]) init() error {
	if _, err := b.db.Exec(`PRAGMA journal_mode = WAL;`); err != nil {
		return err
	}
	if _, err := b.db.Exec(`PRAGMA synchronous = NORMAL;`); err != nil {
		return err
	}
	_, err := b.db.Exec(fmt.Sprintf(`
CREATE TABLE IF NOT EXISTS %s (
	id TEXT PRIMARY KEY,
	job_id TEXT NOT NULL,
	payload BLOB,
	priority INTEGER NOT NULL,
	status TEXT NOT NULL,
	attempts INTEGER NOT NULL,
	available_at INTEGER NOT NULL,
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL,
	completed_at INTEGER
);
CREATE INDEX IF NOT EXISTS idx_%s_status_available ON %s(status, available_at);
CREATE INDEX IF NOT EXISTS idx_%s_priority ON %s(priority, created_at);
`, b.cfg.QueueName, b.cfg.QueueName, b.cfg.QueueName, b.cfg.QueueName, b.cfg.QueueName))
	return err
}

func (b *Backend[T]) tryDequeue(ctx context.Context) (*queueItem[T], error) {
	if err := b.flushBatchIfReady(ctx); err != nil {
		return nil, err
	}

	tx, err := b.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	now := time.Now().UTC().UnixNano()
	row := tx.QueryRowContext(ctx, fmt.Sprintf(`
SELECT id, job_id, payload
FROM %s
WHERE status = 'pending' AND available_at <= ?
ORDER BY priority DESC, created_at ASC
LIMIT 1
`, b.cfg.QueueName), now)

	var item queueItem[T]
	if err := row.Scan(&item.taskID, &item.jobID, &item.payload); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	res, err := tx.ExecContext(ctx, fmt.Sprintf(`
UPDATE %s
SET status = 'in_flight', updated_at = ?
WHERE id = ? AND status = 'pending'
`, b.cfg.QueueName), now, item.taskID)
	if err != nil {
		return nil, err
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		return nil, nil
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return &item, nil
}

func (b *Backend[T]) retryTask(ctx context.Context, taskKey string) error {
	tx, err := b.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var attempts int
	row := tx.QueryRowContext(ctx, fmt.Sprintf(`
SELECT attempts
FROM %s
WHERE id = ? AND status = 'in_flight'
`, b.cfg.QueueName), taskKey)
	if err := row.Scan(&attempts); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("unknown task id: %v", taskKey)
		}
		return err
	}

	attempts++
	if b.cfg.RetryMax >= 0 && attempts > b.cfg.RetryMax {
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
UPDATE %s
SET status = 'failed', updated_at = ?
WHERE id = ?
`, b.cfg.QueueName), time.Now().UTC().UnixNano(), taskKey); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		return ErrRetriesExceeded
	}

	now := time.Now().UTC().UnixNano()
	availableAt := now
	if b.cfg.RetryDelay != nil {
		delay := b.cfg.RetryDelay(attempts)
		if delay > 0 {
			availableAt = time.Now().UTC().Add(delay).UnixNano()
		}
	}

	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
UPDATE %s
SET status = 'pending', attempts = ?, available_at = ?, updated_at = ?
WHERE id = ?
`, b.cfg.QueueName), attempts, availableAt, now, taskKey); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	b.signalLocked()
	return nil
}

func (b *Backend[T]) insertItem(ctx context.Context, item queueItem[T]) error {
	_, err := b.db.ExecContext(ctx, fmt.Sprintf(`
INSERT INTO %s (id, job_id, payload, priority, status, attempts, available_at, created_at, updated_at, completed_at)
VALUES (?, ?, ?, ?, 'pending', ?, ?, ?, ?, NULL)
`, b.cfg.QueueName), item.taskID, item.jobID, item.payload, item.priority, item.attempts, item.availAt, item.createdAt, item.createdAt)
	return err
}

func (b *Backend[T]) flushBatchIfReady(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.batch) == 0 {
		return nil
	}
	return b.flushBatchLocked(ctx)
}

func (b *Backend[T]) flushBatchLocked(ctx context.Context) error {
	if len(b.batch) == 0 {
		if b.batchTimer != nil {
			b.batchTimer.Stop()
			b.batchTimer = nil
		}
		return nil
	}

	tx, err := b.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, fmt.Sprintf(`
INSERT INTO %s (id, job_id, payload, priority, status, attempts, available_at, created_at, updated_at, completed_at)
VALUES (?, ?, ?, ?, 'pending', ?, ?, ?, ?, NULL)
`, b.cfg.QueueName))
	if err != nil {
		tx.Rollback()
		return err
	}
	defer stmt.Close()

	for _, item := range b.batch {
		if _, err := stmt.ExecContext(ctx, item.taskID, item.jobID, item.payload, item.priority, item.attempts, item.availAt, item.createdAt, item.createdAt); err != nil {
			stmt.Close()
			tx.Rollback()
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	b.batch = b.batch[:0]
	if b.batchTimer != nil {
		b.batchTimer.Stop()
		b.batchTimer = nil
	}

	b.signalLocked()
	return nil
}

func (b *Backend[T]) flushBatchFromTimer() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.flushBatchLocked(context.Background()) != nil {
		return
	}
}

func (b *Backend[T]) batchEnabledLocked() bool {
	return b.cfg.BatchMaxSize > 0 || b.cfg.BatchMaxWait > 0
}

func (b *Backend[T]) signalLocked() {
	select {
	case b.signal <- struct{}{}:
	default:
	}
}

func taskIDKey(taskID tasky.TaskID) (string, error) {
	if taskID == nil {
		return "", errors.New("task id is nil")
	}
	return fmt.Sprint(taskID), nil
}

type queueItem[T ~string] struct {
	taskID    string
	jobID     string
	payload   []byte
	priority  int
	attempts  int
	createdAt int64
	availAt   int64
}

var queueNamePattern = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

func validateQueueName(name string) error {
	if name == "" {
		return errors.New("queue name is required")
	}
	if !queueNamePattern.MatchString(name) {
		return fmt.Errorf("invalid queue name: %s", name)
	}
	return nil
}
