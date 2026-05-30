-- name: UpsertPRLatestSnapshot :exec
INSERT INTO pr_latest_snapshot (
  stream_id,
  provider,
  remote_url,
  repo_owner,
  repo_name,
  number,
  head_ref,
  head_sha,
  observed_at,
  snapshot_json
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(stream_id) DO UPDATE SET
  provider = excluded.provider,
  remote_url = excluded.remote_url,
  repo_owner = excluded.repo_owner,
  repo_name = excluded.repo_name,
  number = excluded.number,
  head_ref = excluded.head_ref,
  head_sha = excluded.head_sha,
  observed_at = excluded.observed_at,
  snapshot_json = excluded.snapshot_json;

-- name: GetPRLatestSnapshotByStreamID :one
SELECT *
FROM pr_latest_snapshot
WHERE stream_id = ?;

-- name: GetPRLatestSnapshotByRepoAndNumber :one
SELECT *
FROM pr_latest_snapshot
WHERE provider = ?
  AND repo_owner = ?
  AND repo_name = ?
  AND number = ?;

-- name: ListPRLatestSnapshotsByRemoteAndBranch :many
SELECT *
FROM pr_latest_snapshot
WHERE remote_url = ?
  AND head_ref = ?
ORDER BY observed_at DESC;
