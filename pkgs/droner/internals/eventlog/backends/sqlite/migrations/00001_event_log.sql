-- +goose Up
CREATE TABLE IF NOT EXISTS event_log (
  topic TEXT NOT NULL,
  sequence INTEGER NOT NULL,
  id TEXT NOT NULL UNIQUE,
  stream_id TEXT NOT NULL,
  stream_version INTEGER NOT NULL,
  event_type TEXT NOT NULL,
  schema_version INTEGER NOT NULL,
  occurred_at TEXT NOT NULL,
  causation_id TEXT NOT NULL DEFAULT '',
  correlation_id TEXT NOT NULL DEFAULT '',
  payload BLOB NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS event_log_topic_sequence_idx
  ON event_log(topic, sequence);
CREATE UNIQUE INDEX IF NOT EXISTS event_log_stream_version_idx
  ON event_log(topic, stream_id, stream_version);
CREATE INDEX IF NOT EXISTS event_log_stream_occurred_idx
  ON event_log(topic, stream_id, occurred_at);

CREATE TABLE IF NOT EXISTS event_log_checkpoints (
  topic TEXT NOT NULL,
  subscriber_id TEXT NOT NULL,
  last_sequence INTEGER NOT NULL DEFAULT 0,
  updated_at TEXT NOT NULL,
  PRIMARY KEY(topic, subscriber_id)
);

-- +goose Down
DROP TABLE IF EXISTS event_log_checkpoints;
DROP INDEX IF EXISTS event_log_stream_occurred_idx;
DROP INDEX IF EXISTS event_log_stream_version_idx;
DROP INDEX IF EXISTS event_log_topic_sequence_idx;
DROP TABLE IF EXISTS event_log;
