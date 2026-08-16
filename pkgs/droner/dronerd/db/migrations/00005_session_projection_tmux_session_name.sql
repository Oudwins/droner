-- +goose Up
ALTER TABLE session_projection ADD COLUMN tmux_session_name TEXT;

WITH RECURSIVE projection_paths(stream_id, branch, remaining) AS (
  SELECT stream_id, branch, trim(repo_path, '/')
  FROM session_projection
  WHERE branch IS NOT NULL AND trim(branch) <> ''

  UNION ALL

  SELECT stream_id, branch, substr(remaining, instr(remaining, '/') + 1)
  FROM projection_paths
  WHERE instr(remaining, '/') > 0
),
projection_names(stream_id, tmux_session_name) AS (
  SELECT stream_id, remaining || '#' || branch
  FROM projection_paths
  WHERE instr(remaining, '/') = 0 AND remaining <> ''
)
UPDATE session_projection
SET tmux_session_name = (
  SELECT projection_names.tmux_session_name
  FROM projection_names
  WHERE projection_names.stream_id = session_projection.stream_id
)
WHERE tmux_session_name IS NULL
  AND stream_id IN (SELECT stream_id FROM projection_names);

CREATE INDEX session_projection_tmux_session_name_idx
  ON session_projection(tmux_session_name)
  WHERE tmux_session_name IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS session_projection_tmux_session_name_idx;
ALTER TABLE session_projection DROP COLUMN tmux_session_name;
