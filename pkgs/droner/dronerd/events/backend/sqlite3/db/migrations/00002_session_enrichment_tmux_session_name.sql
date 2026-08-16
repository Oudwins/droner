-- +goose Up
WITH RECURSIVE legacy_enrichments(event_id, branch, remaining) AS (
  SELECT
    enriched.id,
    json_extract(enriched.payload, '$.branch'),
    trim(json_extract(queued.payload, '$.repoPath'), '/')
  FROM event_log AS enriched
  JOIN event_log AS queued
    ON queued.topic = enriched.topic
    AND queued.stream_id = enriched.stream_id
    AND queued.event_type = 'session.queued'
  WHERE enriched.topic = 'sessions'
    AND enriched.event_type = 'session.enrichment.succeeded'
    AND json_valid(enriched.payload)
    AND json_valid(queued.payload)
    AND coalesce(json_extract(enriched.payload, '$.tmuxSessionName'), '') = ''
    AND coalesce(json_extract(enriched.payload, '$.branch'), '') <> ''
    AND coalesce(json_extract(queued.payload, '$.repoPath'), '') <> ''

  UNION ALL

  SELECT event_id, branch, substr(remaining, instr(remaining, '/') + 1)
  FROM legacy_enrichments
  WHERE instr(remaining, '/') > 0
),
session_names(event_id, tmux_session_name) AS (
  SELECT event_id, remaining || '#' || branch
  FROM legacy_enrichments
  WHERE instr(remaining, '/') = 0 AND remaining <> ''
)
UPDATE event_log
SET payload = CAST(json_set(
  payload,
  '$.tmuxSessionName',
  (SELECT session_names.tmux_session_name
   FROM session_names
   WHERE session_names.event_id = event_log.id)
) AS BLOB)
WHERE topic = 'sessions'
  AND id IN (SELECT event_id FROM session_names);

-- +goose Down
SELECT 1;
