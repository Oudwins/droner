package sessionevents

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Oudwins/droner/pkgs/droner/dronerd/sessionslog"
	"github.com/Oudwins/droner/pkgs/droner/internals/backends"
	"github.com/Oudwins/droner/pkgs/droner/internals/conf"
	"github.com/Oudwins/droner/pkgs/droner/internals/eventlog"
	"github.com/Oudwins/droner/pkgs/droner/internals/schemas"

	_ "modernc.org/sqlite"
)

const (
	consumerProjection    = "session_projection"
	consumerCreateProcess = "session_create_process"
)

type System struct {
	db       *sql.DB
	log      eventlog.EventLog
	logger   *slog.Logger
	config   *conf.Config
	backends *backends.Store

	startOnce sync.Once
}

type CreateSessionInput struct {
	StreamID        string
	SimpleID        string
	BackendID       conf.BackendID
	RepoPath        string
	WorktreePath    string
	RemoteURL       string
	AgentConfigJSON string
}

type CreateSessionResult struct {
	TaskID string
}

type ListItem struct {
	SimpleID string
	State    string
}

type TaskSnapshot struct {
	TaskID       string
	Status       schemas.TaskStatus
	SimpleID     string
	WorktreePath string
	Error        string
	CreatedAt    time.Time
	StartedAt    time.Time
	FinishedAt   time.Time
}

func Open(dataDir string, logger *slog.Logger, config *conf.Config, backendStore *backends.Store) (*System, error) {
	cleanDataDir := filepath.Clean(dataDir)
	dbPath := filepath.Join(cleanDataDir, "db", "droner.new.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, err
	}

	conn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = conn.Close()
		}
	}()
	conn.SetMaxOpenConns(1)
	conn.SetMaxIdleConns(1)
	if _, err := conn.Exec("PRAGMA journal_mode = WAL;"); err != nil {
		return nil, err
	}
	if _, err := conn.Exec("PRAGMA busy_timeout = 5000;"); err != nil {
		return nil, err
	}
	if _, err := conn.Exec("PRAGMA synchronous = NORMAL;"); err != nil {
		return nil, err
	}

	log, err := sessionslog.Open(cleanDataDir)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil && log != nil {
			_ = log.Close()
		}
	}()
	for _, stmt := range schemaStatements {
		if _, err := conn.Exec(stmt); err != nil {
			return nil, err
		}
	}

	return &System{db: conn, log: log, logger: logger, config: config, backends: backendStore}, nil
}

func (s *System) Close() error {
	if s == nil {
		return nil
	}
	if s.log != nil {
		if err := s.log.Close(); err != nil {
			return err
		}
	}
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *System) Start(ctx context.Context) {
	if s == nil {
		return
	}
	s.startOnce.Do(func() {
		go s.runSubscription(ctx, consumerProjection, eventlog.Subscription{
			ID: eventlog.SubscriberID(consumerProjection),
			Handle: func(ctx context.Context, evt eventlog.Envelope) error {
				return s.applyProjectionEvent(ctx, evt)
			},
		})
		go s.runSubscription(ctx, consumerCreateProcess, eventlog.Subscription{
			ID: eventlog.SubscriberID(consumerCreateProcess),
			Filter: func(evt eventlog.Envelope) bool {
				return evt.Type == eventTypeSessionQueued
			},
			Handle: func(ctx context.Context, evt eventlog.Envelope) error {
				return s.handleQueuedEvent(ctx, evt)
			},
		})
	})
}

func (s *System) CreateSession(ctx context.Context, input CreateSessionInput) (CreateSessionResult, error) {
	if _, err := s.appendEvent(ctx, input.StreamID, eventTypeSessionQueued, newQueuedPayload(input), "", input.StreamID); err != nil {
		return CreateSessionResult{}, err
	}
	return CreateSessionResult{TaskID: taskIDPrefix + input.StreamID}, nil
}

func (s *System) ListSessions(ctx context.Context, all bool) ([]ListItem, error) {
	query := `SELECT simple_id, public_state FROM session_projection`
	args := []any{}
	if !all {
		query += ` WHERE public_state IN (?, ?)`
		args = append(args, "queued", "running")
	}
	query += ` ORDER BY updated_at DESC LIMIT 100`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []ListItem{}
	for rows.Next() {
		var item ListItem
		if err := rows.Scan(&item.SimpleID, &item.State); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *System) TaskStatus(ctx context.Context, taskID string) (*TaskSnapshot, error) {
	streamID := strings.TrimSpace(strings.TrimPrefix(taskID, taskIDPrefix))
	if streamID == "" || streamID == taskID {
		return nil, sql.ErrNoRows
	}

	projection, err := s.loadProjection(ctx, streamID)
	if err != nil {
		return nil, err
	}

	status, startedAt, finishedAt := projection.taskTimes()
	return &TaskSnapshot{
		TaskID:       taskID,
		Status:       status,
		SimpleID:     projection.SimpleID,
		WorktreePath: projection.WorktreePath,
		Error:        projection.LastError,
		CreatedAt:    projection.CreatedAt,
		StartedAt:    startedAt,
		FinishedAt:   finishedAt,
	}, nil
}

func (s *System) runSubscription(ctx context.Context, consumerName string, sub eventlog.Subscription) {
	for {
		if err := s.log.Subscribe(ctx, sub); err != nil && !errors.Is(err, context.Canceled) {
			s.logger.Error("sessionevents subscription failed", "consumer", consumerName, "error", err)
		} else {
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func (s *System) appendEvent(ctx context.Context, streamID string, eventType eventlog.EventType, payload any, causationID, correlationID string) (eventlog.Envelope, error) {
	pending, err := newPendingEvent(streamID, eventType, payload, causationID, correlationID)
	if err != nil {
		return eventlog.Envelope{}, err
	}
	return s.log.Append(ctx, pending)
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func parseTime(raw string) time.Time {
	if raw == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}
	}
	return parsed
}
