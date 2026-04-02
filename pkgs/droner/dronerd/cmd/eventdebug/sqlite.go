package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const defaultTableName = "event_log"

type SQLiteStoreOptions struct {
	TableName string
}

type SQLiteStore struct {
	db        *sql.DB
	tableName string
}

func OpenSQLite(path string, opts SQLiteStoreOptions) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	return NewSQLiteStore(db, opts), nil
}

func NewSQLiteStore(db *sql.DB, opts SQLiteStoreOptions) *SQLiteStore {
	tableName := strings.TrimSpace(opts.TableName)
	if tableName == "" {
		tableName = defaultTableName
	}
	return &SQLiteStore{db: db, tableName: tableName}
}

func (s *SQLiteStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *SQLiteStore) ListStreams(ctx context.Context, opts ListOptions) ([]StreamSummary, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 100
	}

	query := "%"
	if trimmed := strings.TrimSpace(opts.Query); trimmed != "" {
		query = "%" + trimmed + "%"
	}

	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT stream_id, COUNT(*) AS event_count, MIN(occurred_at), MAX(occurred_at)
		FROM %s
		WHERE stream_id LIKE ?
		GROUP BY stream_id
		ORDER BY MIN(occurred_at) DESC, stream_id ASC
		LIMIT ?
	`, s.tableName), query, limit)
	if err != nil {
		return nil, fmt.Errorf("list streams: %w", err)
	}
	defer rows.Close()

	streams := make([]StreamSummary, 0)
	for rows.Next() {
		var streamID string
		var eventCount int
		var firstRaw any
		var lastRaw any
		if err := rows.Scan(&streamID, &eventCount, &firstRaw, &lastRaw); err != nil {
			return nil, fmt.Errorf("scan stream summary: %w", err)
		}
		firstAt, err := parseSQLiteTime(firstRaw)
		if err != nil {
			return nil, fmt.Errorf("parse first occurred_at for %q: %w", streamID, err)
		}
		lastAt, err := parseSQLiteTime(lastRaw)
		if err != nil {
			return nil, fmt.Errorf("parse last occurred_at for %q: %w", streamID, err)
		}
		streams = append(streams, StreamSummary{
			StreamID:        streamID,
			EventCount:      eventCount,
			FirstOccurredAt: firstAt,
			LastOccurredAt:  lastAt,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate stream summaries: %w", err)
	}

	return streams, nil
}

func (s *SQLiteStore) LoadStream(ctx context.Context, streamID string, opts StreamOptions) (Stream, error) {
	trimmed := strings.TrimSpace(streamID)
	if trimmed == "" {
		return Stream{}, ErrStreamNotFound
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = 500
	}

	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT id, stream_id, stream_version, event_type, schema_version, occurred_at, causation_id, correlation_id, payload
		FROM %s
		WHERE stream_id = ?
		ORDER BY stream_version ASC
		LIMIT ?
	`, s.tableName), trimmed, limit)
	if err != nil {
		return Stream{}, fmt.Errorf("load stream: %w", err)
	}
	defer rows.Close()

	events := make([]Event, 0)
	for rows.Next() {
		var evt Event
		var occurredAtRaw any
		var causationID sql.NullString
		var correlationID sql.NullString
		var payloadRaw any
		if err := rows.Scan(
			&evt.ID,
			&evt.StreamID,
			&evt.StreamVersion,
			&evt.EventType,
			&evt.SchemaVersion,
			&occurredAtRaw,
			&causationID,
			&correlationID,
			&payloadRaw,
		); err != nil {
			return Stream{}, fmt.Errorf("scan event: %w", err)
		}
		occurredAt, err := parseSQLiteTime(occurredAtRaw)
		if err != nil {
			return Stream{}, fmt.Errorf("parse event time: %w", err)
		}
		payload, err := normalizeJSONPayload(payloadRaw)
		if err != nil {
			return Stream{}, fmt.Errorf("normalize payload: %w", err)
		}
		evt.OccurredAt = occurredAt
		evt.CausationID = causationID.String
		evt.CorrelationID = correlationID.String
		evt.Payload = payload
		events = append(events, evt)
	}
	if err := rows.Err(); err != nil {
		return Stream{}, fmt.Errorf("iterate events: %w", err)
	}
	if len(events) == 0 {
		return Stream{}, ErrStreamNotFound
	}

	stream := Stream{
		Summary: StreamSummary{
			StreamID:        trimmed,
			EventCount:      len(events),
			FirstOccurredAt: events[0].OccurredAt,
			LastOccurredAt:  events[len(events)-1].OccurredAt,
		},
		Events: events,
	}

	var totalCount int
	if err := s.db.QueryRowContext(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE stream_id = ?`, s.tableName), trimmed).Scan(&totalCount); err == nil {
		stream.Summary.EventCount = totalCount
	}

	return stream, nil
}

func parseSQLiteTime(raw any) (time.Time, error) {
	switch v := raw.(type) {
	case time.Time:
		return v.UTC(), nil
	case string:
		return parseTimeString(v)
	case []byte:
		return parseTimeString(string(v))
	case int64:
		return time.Unix(0, v).UTC(), nil
	case float64:
		return time.Unix(0, int64(v)).UTC(), nil
	case nil:
		return time.Time{}, nil
	default:
		return time.Time{}, fmt.Errorf("unsupported time type %T", raw)
	}
}

func parseTimeString(value string) (time.Time, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return time.Time{}, nil
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
	}
	for _, layout := range layouts {
		if ts, err := time.Parse(layout, trimmed); err == nil {
			return ts.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported time format %q", trimmed)
}

func normalizeJSONPayload(raw any) (json.RawMessage, error) {
	switch v := raw.(type) {
	case nil:
		return json.RawMessage("null"), nil
	case []byte:
		return normalizeJSONBytes(v)
	case string:
		return normalizeJSONBytes([]byte(v))
	default:
		bytes, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		return normalizeJSONBytes(bytes)
	}
}

func normalizeJSONBytes(raw []byte) (json.RawMessage, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return json.RawMessage("null"), nil
	}
	if !json.Valid([]byte(trimmed)) {
		quoted, err := json.Marshal(trimmed)
		if err != nil {
			return nil, err
		}
		return json.RawMessage(quoted), nil
	}
	return json.RawMessage(trimmed), nil
}
