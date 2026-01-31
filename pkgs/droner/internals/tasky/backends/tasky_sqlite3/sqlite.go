package taskysqlite3

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

type Backend[T tasky.JobID] struct {
	mu         sync.Mutex
	db         *sql.DB
	batch      []queueItem
	batchTimer *time.Timer
	signal     chan struct{}
	cfg        Config
	stmts      *preparedStatements
}

func New[T tasky.JobID](cfg Config) (*Backend[T], error) {
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
	stmts, err := prepareStatements(backend.db, backend.cfg.QueueName)
	if err != nil {
		return nil, err
	}
	backend.stmts = stmts

	return backend, nil
}

func (b *Backend[T]) Enqueue(ctx context.Context, task *tasky.Task[T], job *tasky.Job[T]) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	taskKey, err := taskIDKey(task.TaskID)
	if err != nil {
		return err
	}

	item := queueItem{
		taskID:    taskKey,
		jobID:     string(task.JobID),
		payload:   task.Payload,
		priority:  job.Priority,
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

func (b *Backend[T]) Dequeue(ctx context.Context) (T, tasky.TaskID, []byte, error) {
	timer := time.NewTimer(b.cfg.PollInterval)
	defer timer.Stop()
	for {
		if ctx.Err() != nil {
			var zero T
			return zero, nil, nil, ctx.Err()
		}

		item, err := b.tryDequeue(ctx)
		if err != nil {
			var zero T
			return zero, nil, nil, err
		}
		if item != nil {
			jobID := T(item.jobID)
			return jobID, item.taskID, item.payload, nil
		}

		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(b.cfg.PollInterval)

		select {
		case <-ctx.Done():
			var zero T
			return zero, nil, nil, ctx.Err()
		case <-b.signal:
		case <-timer.C:
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
	rows, err := b.stmts.ack(ctx, now, key)
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
	if b.stmts != nil {
		b.stmts.Close()
	}
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
CREATE INDEX IF NOT EXISTS idx_%s_dequeue ON %s(status, available_at, priority DESC, created_at ASC);
DROP INDEX IF EXISTS idx_%s_priority;
`, b.cfg.QueueName, b.cfg.QueueName, b.cfg.QueueName, b.cfg.QueueName))
	return err
}

func (b *Backend[T]) tryDequeue(ctx context.Context) (*queueItem, error) {
	if err := b.flushBatchIfReady(ctx); err != nil {
		return nil, err
	}

	now := time.Now().UTC().UnixNano()
	item, err := b.stmts.dequeue(ctx, now)
	if err != nil {
		return nil, err
	}
	return item, nil
}

func (b *Backend[T]) retryTask(ctx context.Context, taskKey string) error {
	tx, err := b.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var attempts int
	attempts, err = b.stmts.retrySelect(ctx, tx, taskKey)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("unknown task id: %v", taskKey)
		}
		return err
	}

	attempts++
	if b.cfg.RetryMax >= 0 && attempts > b.cfg.RetryMax {
		if err := b.stmts.retryFail(ctx, tx, time.Now().UTC().UnixNano(), taskKey); err != nil {
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

	if err := b.stmts.retryPending(ctx, tx, attempts, availableAt, now, taskKey); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	b.signalLocked()
	return nil
}

func (b *Backend[T]) insertItem(ctx context.Context, item queueItem) error {
	return b.stmts.insert(ctx, item)
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

	maxParams := 999
	paramsPerRow := 8
	maxRows := maxParams / paramsPerRow
	for start := 0; start < len(b.batch); start += maxRows {
		end := start + maxRows
		if end > len(b.batch) {
			end = len(b.batch)
		}
		args := make([]any, 0, (end-start)*paramsPerRow)
		placeholders := make([]byte, 0, (end-start)*48)
		for i := start; i < end; i++ {
			item := b.batch[i]
			if i > start {
				placeholders = append(placeholders, ',')
			}
			placeholders = append(placeholders, '(', '?', ',', ' ', '?', ',', ' ', '?', ',', ' ', '?', ',', ' ', '\'', 'p', 'e', 'n', 'd', 'i', 'n', 'g', '\'', ',', ' ', '?', ',', ' ', '?', ',', ' ', '?', ',', ' ', '?', ',', ' ', 'N', 'U', 'L', 'L', ')')
			args = append(args, item.taskID, item.jobID, item.payload, item.priority, item.attempts, item.availAt, item.createdAt, item.createdAt)
		}
		query := fmt.Sprintf(`
INSERT INTO %s (id, job_id, payload, priority, status, attempts, available_at, created_at, updated_at, completed_at)
VALUES %s
`, b.cfg.QueueName, string(placeholders))
		if _, err := tx.ExecContext(ctx, query, args...); err != nil {
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

type queueItem struct {
	taskID    string
	jobID     string
	payload   []byte
	priority  int
	attempts  int
	createdAt int64
	availAt   int64
}

type preparedStatements struct {
	stmtDequeue      *sql.Stmt
	stmtInsert       *sql.Stmt
	stmtAck          *sql.Stmt
	stmtRetrySelect  *sql.Stmt
	stmtRetryFail    *sql.Stmt
	stmtRetryPending *sql.Stmt
}

func prepareStatements(db *sql.DB, queueName string) (*preparedStatements, error) {
	dequeueSQL := fmt.Sprintf(`
WITH next AS (
 SELECT id
 FROM %s
 WHERE status = 'pending' AND available_at <= ?
 ORDER BY priority DESC, created_at ASC
 LIMIT 1
)
UPDATE %s
SET status = 'in_flight', updated_at = ?
WHERE id IN (SELECT id FROM next)
RETURNING id, job_id, payload
`, queueName, queueName)
	insertSQL := fmt.Sprintf(`
INSERT INTO %s (id, job_id, payload, priority, status, attempts, available_at, created_at, updated_at, completed_at)
VALUES (?, ?, ?, ?, 'pending', ?, ?, ?, ?, NULL)
`, queueName)
	ackSQL := fmt.Sprintf(`
UPDATE %s
SET status = 'completed', updated_at = ?, completed_at = ?
WHERE id = ?
`, queueName)
	retrySelectSQL := fmt.Sprintf(`
SELECT attempts
FROM %s
WHERE id = ? AND status = 'in_flight'
`, queueName)
	retryFailSQL := fmt.Sprintf(`
UPDATE %s
SET status = 'failed', updated_at = ?
WHERE id = ?
`, queueName)
	retryPendingSQL := fmt.Sprintf(`
UPDATE %s
SET status = 'pending', attempts = ?, available_at = ?, updated_at = ?
WHERE id = ?
`, queueName)

	var err error
	stmts := &preparedStatements{}
	stmts.stmtDequeue, err = db.Prepare(dequeueSQL)
	if err != nil {
		stmts.Close()
		return nil, err
	}
	stmts.stmtInsert, err = db.Prepare(insertSQL)
	if err != nil {
		stmts.Close()
		return nil, err
	}
	stmts.stmtAck, err = db.Prepare(ackSQL)
	if err != nil {
		stmts.Close()
		return nil, err
	}
	stmts.stmtRetrySelect, err = db.Prepare(retrySelectSQL)
	if err != nil {
		stmts.Close()
		return nil, err
	}
	stmts.stmtRetryFail, err = db.Prepare(retryFailSQL)
	if err != nil {
		stmts.Close()
		return nil, err
	}
	stmts.stmtRetryPending, err = db.Prepare(retryPendingSQL)
	if err != nil {
		stmts.Close()
		return nil, err
	}

	return stmts, nil
}

func (s *preparedStatements) Close() {
	if s == nil {
		return
	}
	if s.stmtDequeue != nil {
		s.stmtDequeue.Close()
	}
	if s.stmtInsert != nil {
		s.stmtInsert.Close()
	}
	if s.stmtAck != nil {
		s.stmtAck.Close()
	}
	if s.stmtRetrySelect != nil {
		s.stmtRetrySelect.Close()
	}
	if s.stmtRetryFail != nil {
		s.stmtRetryFail.Close()
	}
	if s.stmtRetryPending != nil {
		s.stmtRetryPending.Close()
	}
}

func (s *preparedStatements) dequeue(ctx context.Context, now int64) (*queueItem, error) {
	row := s.stmtDequeue.QueryRowContext(ctx, now, now)
	var item queueItem
	if err := row.Scan(&item.taskID, &item.jobID, &item.payload); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &item, nil
}

func (s *preparedStatements) insert(ctx context.Context, item queueItem) error {
	_, err := s.stmtInsert.ExecContext(ctx, item.taskID, item.jobID, item.payload, item.priority, item.attempts, item.availAt, item.createdAt, item.createdAt)
	return err
}

func (s *preparedStatements) ack(ctx context.Context, now int64, taskKey string) (int64, error) {
	res, err := s.stmtAck.ExecContext(ctx, now, now, taskKey)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *preparedStatements) retrySelect(ctx context.Context, tx *sql.Tx, taskKey string) (int, error) {
	row := tx.StmtContext(ctx, s.stmtRetrySelect).QueryRowContext(ctx, taskKey)
	var attempts int
	if err := row.Scan(&attempts); err != nil {
		return 0, err
	}
	return attempts, nil
}

func (s *preparedStatements) retryFail(ctx context.Context, tx *sql.Tx, now int64, taskKey string) error {
	_, err := tx.StmtContext(ctx, s.stmtRetryFail).ExecContext(ctx, now, taskKey)
	return err
}

func (s *preparedStatements) retryPending(ctx context.Context, tx *sql.Tx, attempts int, availableAt int64, now int64, taskKey string) error {
	_, err := tx.StmtContext(ctx, s.stmtRetryPending).ExecContext(ctx, attempts, availableAt, now, taskKey)
	return err
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
