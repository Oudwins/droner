-- +goose Up
CREATE TABLE IF NOT EXISTS session_projection (
  stream_id TEXT PRIMARY KEY,
  branch TEXT NOT NULL UNIQUE,
  backend_id TEXT NOT NULL,
  repo_path TEXT NOT NULL,
  worktree_path TEXT NOT NULL,
  remote_url TEXT NOT NULL DEFAULT '',
  agent_config TEXT NOT NULL DEFAULT '',
  lifecycle_state TEXT NOT NULL,
  public_state TEXT NOT NULL,
  last_error TEXT NOT NULL DEFAULT '',
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL
);

CREATE INDEX IF NOT EXISTS session_projection_public_state_idx
  ON session_projection(public_state, updated_at DESC);

-- +goose Down
DROP INDEX IF EXISTS session_projection_public_state_idx;
DROP TABLE IF EXISTS session_projection;
