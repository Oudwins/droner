CREATE TABLE IF NOT EXISTS sessions (
  id TEXT PRIMARY KEY,
  simple_id TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('queued', 'running', 'completed', 'failed', 'deleted')),
  repo_path TEXT NOT NULL,
  worktree_path TEXT NOT NULL,
  agent_config TEXT,
  error TEXT,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS sessions_simple_id_active_uniq
  ON sessions(simple_id)
  WHERE status IN ('queued', 'running', 'completed');

CREATE INDEX IF NOT EXISTS sessions_status_idx
  ON sessions(status);

CREATE INDEX IF NOT EXISTS sessions_updated_at_idx
  ON sessions(updated_at);
