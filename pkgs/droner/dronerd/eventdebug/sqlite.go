package eventdebug

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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
	topics := normalizeTopics(opts.Topics)

	query := "%"
	if trimmed := strings.TrimSpace(opts.Query); trimmed != "" {
		query = "%" + trimmed + "%"
	}
	where := "stream_id LIKE ?"
	args := []any{query}
	if len(topics) > 0 {
		where = fmt.Sprintf("topic IN (%s) AND stream_id LIKE ?", queryPlaceholders(len(topics)))
		args = make([]any, 0, len(topics)+2)
		for _, topic := range topics {
			args = append(args, topic)
		}
		args = append(args, query)
	}
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT topic, stream_id, COUNT(*) AS event_count, MIN(occurred_at), MAX(occurred_at)
		FROM %s
		WHERE %s
		GROUP BY topic, stream_id
		ORDER BY MAX(occurred_at) DESC, topic ASC, stream_id ASC
		LIMIT ?
	`, s.tableName, where), args...)
	if err != nil {
		return nil, fmt.Errorf("list streams: %w", err)
	}
	defer rows.Close()

	streams := make([]StreamSummary, 0)
	for rows.Next() {
		var topic string
		var streamID string
		var eventCount int
		var firstRaw any
		var lastRaw any
		if err := rows.Scan(&topic, &streamID, &eventCount, &firstRaw, &lastRaw); err != nil {
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
			Topic:           topic,
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
	topic := normalizeTopic(opts.Topic)
	if topic == topicAll {
		resolvedTopic, err := s.resolveStreamTopic(ctx, trimmed)
		if err != nil {
			return Stream{}, err
		}
		topic = resolvedTopic
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = 500
	}

	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT id, topic, stream_id, stream_version, event_type, schema_version, occurred_at, causation_id, correlation_id, payload
		FROM %s
		WHERE topic = ? AND stream_id = ?
		ORDER BY stream_version ASC
		LIMIT ?
	`, s.tableName), topic, trimmed, limit)
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
			&evt.Topic,
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
		Topic: topic,
		Summary: StreamSummary{
			Topic:           topic,
			StreamID:        trimmed,
			EventCount:      len(events),
			FirstOccurredAt: events[0].OccurredAt,
			LastOccurredAt:  events[len(events)-1].OccurredAt,
		},
		Events: events,
	}

	var totalCount int
	if err := s.db.QueryRowContext(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE topic = ? AND stream_id = ?`, s.tableName), topic, trimmed).Scan(&totalCount); err == nil {
		stream.Summary.EventCount = totalCount
	}

	return stream, nil
}

func (s *SQLiteStore) resolveStreamTopic(ctx context.Context, streamID string) (string, error) {
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT DISTINCT topic
		FROM %s
		WHERE stream_id = ?
		ORDER BY topic ASC
	`, s.tableName), streamID)
	if err != nil {
		return "", fmt.Errorf("resolve stream topic: %w", err)
	}
	defer rows.Close()

	topics := make([]string, 0, 1)
	for rows.Next() {
		var topic string
		if err := rows.Scan(&topic); err != nil {
			return "", fmt.Errorf("scan stream topic: %w", err)
		}
		topics = append(topics, topic)
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("iterate stream topics: %w", err)
	}
	if len(topics) == 0 {
		return "", ErrStreamNotFound
	}
	if len(topics) > 1 {
		return "", fmt.Errorf("stream %q exists in multiple topics (%s); select a topic", streamID, strings.Join(topics, ", "))
	}
	return topics[0], nil
}

func normalizeTopic(value string) string {
	switch strings.TrimSpace(value) {
	case "", topicAll:
		return topicAll
	case topicSessions:
		return topicSessions
	case topicPullRequests:
		return topicPullRequests
	default:
		return topicAll
	}
}

func normalizeTopics(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := map[string]bool{}
	topics := make([]string, 0, len(values))
	for _, value := range values {
		topic := normalizeTopic(value)
		if topic == topicAll {
			return nil
		}
		if seen[topic] {
			continue
		}
		seen[topic] = true
		topics = append(topics, topic)
	}
	return topics
}

func topicSelected(topics []string, topic string) bool {
	normalized := normalizeTopics(topics)
	if topic == topicAll {
		return len(normalized) == 0
	}
	for _, selected := range normalized {
		if selected == topic {
			return true
		}
	}
	return false
}

func queryPlaceholders(count int) string {
	if count <= 0 {
		return ""
	}
	return strings.TrimRight(strings.Repeat("?,", count), ",")
}

func isAmbiguousStreamError(err error) bool {
	return err != nil && !errors.Is(err, ErrStreamNotFound) && strings.Contains(err.Error(), "exists in multiple topics")
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
