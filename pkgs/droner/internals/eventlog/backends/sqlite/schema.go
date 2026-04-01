package sqliteeventlog

import (
	"fmt"
	"regexp"
)

const (
	defaultEventTable      = "event_log"
	defaultCheckpointTable = "event_log_checkpoints"
)

var validNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func validateName(name string) error {
	if !validNamePattern.MatchString(name) {
		return fmt.Errorf("invalid sqlite identifier: %q", name)
	}
	return nil
}

func eventTableDDL(eventTable string) string {
	return fmt.Sprintf(`
CREATE TABLE IF NOT EXISTS %s (
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
CREATE UNIQUE INDEX IF NOT EXISTS %s_topic_sequence_idx ON %s(topic, sequence);
CREATE UNIQUE INDEX IF NOT EXISTS %s_stream_version_idx ON %s(topic, stream_id, stream_version);
CREATE INDEX IF NOT EXISTS %s_stream_occurred_idx ON %s(topic, stream_id, occurred_at);
	`, eventTable, eventTable, eventTable, eventTable, eventTable, eventTable, eventTable)
}

func checkpointTableDDL(checkpointTable string) string {
	return fmt.Sprintf(`
CREATE TABLE IF NOT EXISTS %s (
	topic TEXT NOT NULL,
	subscriber_id TEXT NOT NULL,
	last_sequence INTEGER NOT NULL DEFAULT 0,
	updated_at TEXT NOT NULL,
	PRIMARY KEY(topic, subscriber_id)
);
`, checkpointTable)
}
