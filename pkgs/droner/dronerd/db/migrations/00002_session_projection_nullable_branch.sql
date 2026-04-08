-- +goose Up
DROP INDEX IF EXISTS session_projection_public_state_idx;

ALTER TABLE session_projection RENAME TO session_projection_old;

CREATE TABLE session_projection (
  stream_id TEXT PRIMARY KEY,
  harness TEXT NOT NULL,
  branch TEXT,
  backend_id TEXT NOT NULL,
  repo_path TEXT NOT NULL,
  worktree_path TEXT,
  remote_url TEXT NOT NULL DEFAULT '',
  agent_config TEXT NOT NULL DEFAULT '',
  lifecycle_state TEXT NOT NULL,
  public_state TEXT NOT NULL,
  last_error TEXT NOT NULL DEFAULT '',
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL
);

INSERT INTO session_projection (
  stream_id,
  harness,
  branch,
  backend_id,
  repo_path,
  worktree_path,
  remote_url,
  agent_config,
  lifecycle_state,
  public_state,
  last_error,
  created_at,
  updated_at
)
SELECT
  stream_id,
  harness,
  branch,
  backend_id,
  repo_path,
  worktree_path,
  remote_url,
  agent_config,
  lifecycle_state,
  public_state,
  last_error,
  created_at,
  updated_at
FROM session_projection_old;

DROP TABLE session_projection_old;

CREATE INDEX session_projection_public_state_idx
  ON session_projection(public_state, updated_at DESC);

CREATE UNIQUE INDEX session_projection_active_branch_unique_idx
  ON session_projection(branch)
  WHERE branch IS NOT NULL
    AND public_state IN ('queued', 'active.idle', 'active.busy', 'completing', 'deleting');

-- +goose Down
DROP INDEX IF EXISTS session_projection_active_branch_unique_idx;
DROP INDEX IF EXISTS session_projection_public_state_idx;

ALTER TABLE session_projection RENAME TO session_projection_new;

CREATE TABLE session_projection (
  stream_id TEXT PRIMARY KEY,
  harness TEXT NOT NULL,
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

INSERT INTO session_projection (
  stream_id,
  harness,
  branch,
  backend_id,
  repo_path,
  worktree_path,
  remote_url,
  agent_config,
  lifecycle_state,
  public_state,
  last_error,
  created_at,
  updated_at
)
SELECT
  stream_id,
  harness,
  COALESCE(branch, ''),
  backend_id,
  repo_path,
  COALESCE(worktree_path, ''),
  remote_url,
  agent_config,
  lifecycle_state,
  public_state,
  last_error,
  created_at,
  updated_at
FROM session_projection_new;

DROP TABLE session_projection_new;

CREATE INDEX session_projection_public_state_idx
  ON session_projection(public_state, updated_at DESC);
