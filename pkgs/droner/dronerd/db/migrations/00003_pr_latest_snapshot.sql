-- +goose Up
CREATE TABLE IF NOT EXISTS pr_latest_snapshot (
  stream_id TEXT PRIMARY KEY,
  provider TEXT NOT NULL,
  remote_url TEXT NOT NULL,
  repo_owner TEXT NOT NULL,
  repo_name TEXT NOT NULL,
  number INTEGER NOT NULL,
  head_ref TEXT NOT NULL,
  head_sha TEXT NOT NULL,
  observed_at DATETIME NOT NULL,
  snapshot_json TEXT NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS pr_latest_snapshot_repo_number_idx
  ON pr_latest_snapshot(provider, repo_owner, repo_name, number);

CREATE INDEX IF NOT EXISTS pr_latest_snapshot_branch_idx
  ON pr_latest_snapshot(remote_url, head_ref, observed_at DESC);

-- +goose Down
DROP INDEX IF EXISTS pr_latest_snapshot_branch_idx;
DROP INDEX IF EXISTS pr_latest_snapshot_repo_number_idx;
DROP TABLE IF EXISTS pr_latest_snapshot;
