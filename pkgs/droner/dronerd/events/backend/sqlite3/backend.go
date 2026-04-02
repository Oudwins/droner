package sqlite3

import (
	"context"
	"database/sql"
	"errors"
	"time"

	backenddb "github.com/Oudwins/droner/pkgs/droner/dronerd/events/backend/sqlite3/db"
	"github.com/Oudwins/droner/pkgs/droner/internals/eventlog"
	"github.com/google/uuid"
)

type Config struct {
	Path         string
	DB           *sql.DB
	PollInterval time.Duration
}

type Backend struct {
	db        *sql.DB
	queries   *backenddb.Queries
	ownsDB    bool
	pollEvery time.Duration
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
		opened, err := backenddb.OpenSQLiteDB(cfg.Path)
		if err != nil {
			return nil, err
		}
		db = opened
		ownsDB = true
	} else if err := backenddb.ConfigureSQLiteDB(db); err != nil {
		return nil, err
	} else if err := backenddb.EnsureMigrations(context.Background(), db); err != nil {
		return nil, err
	}

	return &Backend{
		db:        db,
		queries:   backenddb.New(db),
		ownsDB:    ownsDB,
		pollEvery: cfg.PollInterval,
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

	qtx := b.queries.WithTx(tx)
	sequence, err := qtx.GetNextTopicSequence(ctx, string(topic))
	if err != nil {
		return eventlog.Envelope{}, err
	}
	streamVersion, err := qtx.GetNextStreamVersion(ctx, backenddb.GetNextStreamVersionParams{
		Topic:    string(topic),
		StreamID: string(evt.StreamID),
	})
	if err != nil {
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

	if err = qtx.InsertEvent(ctx, backenddb.InsertEventParams{
		Topic:         string(envelope.Topic),
		Sequence:      envelope.Sequence,
		ID:            string(envelope.ID),
		StreamID:      string(envelope.StreamID),
		StreamVersion: envelope.StreamVersion,
		EventType:     string(envelope.Type),
		SchemaVersion: int64(envelope.SchemaVersion),
		OccurredAt:    formatTime(envelope.OccurredAt),
		CausationID:   string(envelope.CausationID),
		CorrelationID: envelope.CorrelationID,
		Payload:       envelope.Payload,
	}); err != nil {
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
	rows, err := b.queries.LoadStreamEvents(ctx, backenddb.LoadStreamEventsParams{
		Topic:         string(topic),
		StreamID:      string(streamID),
		StreamVersion: opts.AfterVersion,
		Limit:         int64(limit),
	})
	if err != nil {
		return nil, err
	}
	return scanEnvelopes(topic, rows), nil
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
	sequence, err := b.queries.GetCheckpoint(ctx, backenddb.GetCheckpointParams{
		Topic:        string(topic),
		SubscriberID: string(subscriber),
	})
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return sequence, err
}

func (b *Backend) StoreCheckpoint(ctx context.Context, topic eventlog.Topic, subscriber eventlog.SubscriberID, sequence int64) error {
	return b.queries.UpsertCheckpoint(ctx, backenddb.UpsertCheckpointParams{
		Topic:        string(topic),
		SubscriberID: string(subscriber),
		LastSequence: sequence,
		UpdatedAt:    formatTime(time.Now().UTC()),
	})
}

func (b *Backend) Close() error {
	if !b.ownsDB {
		return nil
	}
	return b.db.Close()
}

func (b *Backend) readAvailable(ctx context.Context, topic eventlog.Topic, afterSequence int64, limit int) ([]eventlog.Envelope, error) {
	rows, err := b.queries.ReadGlobalEvents(ctx, backenddb.ReadGlobalEventsParams{
		Topic:    string(topic),
		Sequence: afterSequence,
		Limit:    int64(limit),
	})
	if err != nil {
		return nil, err
	}
	return scanEnvelopes(topic, rows), nil
}

func scanEnvelopes(topic eventlog.Topic, rows []backenddb.EventLog) []eventlog.Envelope {
	items := make([]eventlog.Envelope, 0, len(rows))
	for _, row := range rows {
		items = append(items, eventlog.Envelope{
			ID:            eventlog.EventID(row.ID),
			Topic:         topic,
			StreamID:      eventlog.StreamID(row.StreamID),
			StreamVersion: row.StreamVersion,
			Sequence:      row.Sequence,
			Type:          eventlog.EventType(row.EventType),
			SchemaVersion: int(row.SchemaVersion),
			OccurredAt:    parseTime(row.OccurredAt),
			CausationID:   eventlog.EventID(row.CausationID),
			CorrelationID: row.CorrelationID,
			Payload:       clonePayload(row.Payload),
		})
	}
	return items
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
