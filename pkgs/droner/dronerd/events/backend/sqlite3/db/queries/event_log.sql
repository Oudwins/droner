-- name: GetNextTopicSequence :one
SELECT COALESCE(MAX(sequence), 0) + 1
FROM event_log
WHERE topic = ?;

-- name: GetNextStreamVersion :one
SELECT COALESCE(MAX(stream_version), 0) + 1
FROM event_log
WHERE topic = ?
  AND stream_id = ?;

-- name: InsertEvent :exec
INSERT INTO event_log (
  topic,
  sequence,
  id,
  stream_id,
  stream_version,
  event_type,
  schema_version,
  occurred_at,
  causation_id,
  correlation_id,
  payload
) VALUES (
  ?,
  ?,
  ?,
  ?,
  ?,
  ?,
  ?,
  ?,
  ?,
  ?,
  ?
);

-- name: LoadStreamEvents :many
SELECT *
FROM event_log
WHERE topic = ?
  AND stream_id = ?
  AND stream_version > ?
ORDER BY stream_version ASC
LIMIT ?;

-- name: ReadGlobalEvents :many
SELECT *
FROM event_log
WHERE topic = ?
  AND sequence > ?
ORDER BY sequence ASC
LIMIT ?;
