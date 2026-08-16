-- +goose Up
ALTER TABLE session_projection ADD COLUMN tmux_session_name TEXT;

CREATE INDEX session_projection_tmux_session_name_idx
  ON session_projection(tmux_session_name)
  WHERE tmux_session_name IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS session_projection_tmux_session_name_idx;
ALTER TABLE session_projection DROP COLUMN tmux_session_name;
