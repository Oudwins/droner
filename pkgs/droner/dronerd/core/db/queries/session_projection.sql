-- name: UpsertSessionProjection :exec
INSERT INTO session_projection (
  stream_id,
  simple_id,
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
  ?,
  ?
)
ON CONFLICT(stream_id) DO UPDATE SET
  simple_id = excluded.simple_id,
  backend_id = excluded.backend_id,
  repo_path = excluded.repo_path,
  worktree_path = excluded.worktree_path,
  remote_url = excluded.remote_url,
  agent_config = excluded.agent_config,
  lifecycle_state = excluded.lifecycle_state,
  public_state = excluded.public_state,
  last_error = excluded.last_error,
  updated_at = excluded.updated_at;

-- name: PatchSessionProjection :exec
UPDATE session_projection
SET lifecycle_state = ?,
    public_state = ?,
    last_error = ?,
    updated_at = ?
WHERE stream_id = ?;

-- name: GetSessionProjectionByStreamID :one
SELECT *
FROM session_projection
WHERE stream_id = ?;

-- name: GetSessionProjectionBySimpleID :one
SELECT *
FROM session_projection
WHERE simple_id = ?;

-- name: ListVisibleSessionProjectionItems :many
SELECT simple_id, public_state
FROM session_projection
WHERE public_state IN ('queued', 'running', 'completing')
ORDER BY updated_at DESC
LIMIT 100;

-- name: ListAllSessionProjectionItems :many
SELECT simple_id, public_state
FROM session_projection
ORDER BY updated_at DESC
LIMIT 100;

-- name: ListActiveSessionProjectionRefs :many
SELECT *
FROM session_projection
WHERE public_state IN ('queued', 'running')
ORDER BY updated_at DESC;

-- name: ListHydratableSessionProjectionRefs :many
SELECT *
FROM session_projection
WHERE public_state IN ('queued', 'running', 'completing', 'deleting')
ORDER BY updated_at DESC;
