-- name: GetCheckpoint :one
SELECT last_sequence
FROM event_log_checkpoints
WHERE topic = ?
  AND subscriber_id = ?;

-- name: GetMaxCheckpointSequenceForTopic :one
SELECT COALESCE(MAX(last_sequence), 0)
FROM event_log_checkpoints
WHERE topic = ?;

-- name: UpsertCheckpoint :exec
INSERT INTO event_log_checkpoints (
  topic,
  subscriber_id,
  last_sequence,
  updated_at
) VALUES (
  ?,
  ?,
  ?,
  ?
)
ON CONFLICT(topic, subscriber_id) DO UPDATE SET
  last_sequence = excluded.last_sequence,
  updated_at = excluded.updated_at;
