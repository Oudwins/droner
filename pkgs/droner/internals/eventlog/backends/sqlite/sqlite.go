package sqliteeventlog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Oudwins/droner/pkgs/droner/internals/eventlog"
	"github.com/google/uuid"

	_ "modernc.org/sqlite"
)

type Config struct {
	Path         string
	DB           *sql.DB
	PollInterval time.Duration
}

type Backend struct {
	db         *sql.DB
	eventTable string
	checkTable string
	ownsDB     bool
	pollEvery  time.Duration
}

func New(cfg Config) (*Backend, error) {
	if cfg.DB == nil && cfg.Path == "" {
		return nil, errors.New("sqlite eventlog requires a db or path")
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 200 * time.Millisecond
	}

	db := cfg.DB
	ownsDB := false
	if db == nil {
		opened, err := OpenDB(cfg.Path)
		if err != nil {
			return nil, err
		}
		db = opened
		ownsDB = true
	} else if err := configureDB(db); err != nil {
		return nil, err
	}

	if err := ensureMigrations(context.Background(), db); err != nil {
		if ownsDB {
			_ = db.Close()
		}
		return nil, err
	}

	return &Backend{
		db:         db,
		eventTable: defaultEventTable,
		checkTable: defaultCheckpointTable,
		ownsDB:     ownsDB,
		pollEvery:  cfg.PollInterval,
	}, nil
}

func (b *Backend) Append(ctx context.Context, topic eventlog.Topic, evt eventlog.PendingEvent) (eventlog.Envelope, error) {
	tx, err := b.db.BeginTx(ctx, nil)
	if err != nil {
		return eventlog.Envelope{}, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	var sequence int64
	if err = tx.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT COALESCE(MAX(sequence), 0) + 1 FROM %s WHERE topic = ?`, b.eventTable),
		string(topic),
	).Scan(&sequence); err != nil {
		return eventlog.Envelope{}, err
	}

	var streamVersion int64
	if err = tx.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT COALESCE(MAX(stream_version), 0) + 1 FROM %s WHERE topic = ? AND stream_id = ?`, b.eventTable),
		string(topic), string(evt.StreamID),
	).Scan(&streamVersion); err != nil {
		return eventlog.Envelope{}, err
	}

	occurredAt := time.Now().UTC()
	envelope := eventlog.Envelope{
		ID:            eventlog.EventID(uuid.NewString()),
		Topic:         topic,
		StreamID:      evt.StreamID,
		StreamVersion: streamVersion,
		Sequence:      sequence,
		Type:          evt.Type,
		SchemaVersion: evt.SchemaVersion,
		OccurredAt:    occurredAt,
		CausationID:   evt.CausationID,
		CorrelationID: evt.CorrelationID,
		Payload:       clonePayload(evt.Payload),
	}

	if _, err = tx.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO %s (
			topic, sequence, id, stream_id, stream_version, event_type, schema_version,
			occurred_at, causation_id, correlation_id, payload
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, b.eventTable),
		string(envelope.Topic),
		envelope.Sequence,
		string(envelope.ID),
		string(envelope.StreamID),
		envelope.StreamVersion,
		string(envelope.Type),
		envelope.SchemaVersion,
		formatTime(envelope.OccurredAt),
		string(envelope.CausationID),
		envelope.CorrelationID,
		envelope.Payload,
	); err != nil {
		return eventlog.Envelope{}, err
	}

	if err = tx.Commit(); err != nil {
		return eventlog.Envelope{}, err
	}
	return envelope, nil
}

func (b *Backend) LoadStream(ctx context.Context, topic eventlog.Topic, streamID eventlog.StreamID, opts eventlog.LoadStreamOptions) ([]eventlog.Envelope, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 500
	}
	rows, err := b.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT id, stream_id, stream_version, sequence, event_type, schema_version, occurred_at, causation_id, correlation_id, payload
		FROM %s
		WHERE topic = ? AND stream_id = ? AND stream_version > ?
		ORDER BY stream_version ASC
		LIMIT ?
	`, b.eventTable), string(topic), string(streamID), opts.AfterVersion, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEnvelopes(rows, topic)
}

func (b *Backend) ReadGlobal(ctx context.Context, topic eventlog.Topic, afterSequence int64, limit int) ([]eventlog.Envelope, error) {
	if limit <= 0 {
		limit = 64
	}
	for {
		events, err := b.readAvailable(ctx, topic, afterSequence, limit)
		if err != nil {
			return nil, err
		}
		if len(events) > 0 {
			return events, nil
		}

		timer := time.NewTimer(b.pollEvery)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func (b *Backend) LoadCheckpoint(ctx context.Context, topic eventlog.Topic, subscriber eventlog.SubscriberID) (int64, error) {
	var sequence int64
	err := b.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT last_sequence FROM %s WHERE topic = ? AND subscriber_id = ?`, b.checkTable),
		string(topic), string(subscriber),
	).Scan(&sequence)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return sequence, err
}

func (b *Backend) StoreCheckpoint(ctx context.Context, topic eventlog.Topic, subscriber eventlog.SubscriberID, sequence int64) error {
	_, err := b.db.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO %s (topic, subscriber_id, last_sequence, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(topic, subscriber_id) DO UPDATE SET
			last_sequence = excluded.last_sequence,
			updated_at = excluded.updated_at
	`, b.checkTable), string(topic), string(subscriber), sequence, formatTime(time.Now().UTC()))
	return err
}

func (b *Backend) Close() error {
	if !b.ownsDB {
		return nil
	}
	return b.db.Close()
}

func (b *Backend) readAvailable(ctx context.Context, topic eventlog.Topic, afterSequence int64, limit int) ([]eventlog.Envelope, error) {
	rows, err := b.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT id, stream_id, stream_version, sequence, event_type, schema_version, occurred_at, causation_id, correlation_id, payload
		FROM %s
		WHERE topic = ? AND sequence > ?
		ORDER BY sequence ASC
		LIMIT ?
	`, b.eventTable), string(topic), afterSequence, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEnvelopes(rows, topic)
}

func scanEnvelopes(rows *sql.Rows, topic eventlog.Topic) ([]eventlog.Envelope, error) {
	items := []eventlog.Envelope{}
	for rows.Next() {
		var envelope eventlog.Envelope
		var occurredAt string
		var causationID string
		var payload []byte
		if err := rows.Scan(
			&envelope.ID,
			&envelope.StreamID,
			&envelope.StreamVersion,
			&envelope.Sequence,
			&envelope.Type,
			&envelope.SchemaVersion,
			&occurredAt,
			&causationID,
			&envelope.CorrelationID,
			&payload,
		); err != nil {
			return nil, err
		}
		envelope.Topic = topic
		envelope.CausationID = eventlog.EventID(causationID)
		envelope.OccurredAt = parseTime(occurredAt)
		envelope.Payload = clonePayload(payload)
		items = append(items, envelope)
	}
	return items, rows.Err()
}

func clonePayload(payload []byte) []byte {
	if payload == nil {
		return []byte("null")
	}
	return append([]byte(nil), payload...)
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
