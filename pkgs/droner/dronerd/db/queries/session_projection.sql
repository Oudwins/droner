-- name: UpsertSessionProjection :exec
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
  ?,
  ?
)
ON CONFLICT(stream_id) DO UPDATE SET
  harness = excluded.harness,
  branch = excluded.branch,
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

-- name: GetSessionProjectionByBranch :one
SELECT *
FROM session_projection
WHERE branch = ?;

-- name: ListVisibleSessionProjectionItems :many
SELECT stream_id, repo_path, remote_url, branch, public_state
FROM session_projection
WHERE public_state IN ('queued', 'running', 'completing')
ORDER BY updated_at DESC
LIMIT 100;

-- name: ListAllSessionProjectionItems :many
SELECT stream_id, repo_path, remote_url, branch, public_state
FROM session_projection
ORDER BY updated_at DESC
LIMIT 100;

-- name: ListSessionProjectionItemsAfterCursorByStatuses :many
SELECT stream_id, repo_path, remote_url, branch, public_state
FROM session_projection
WHERE ((? = '') OR (',' || ? || ',') LIKE '%,' || public_state || ',%')
  AND (? = '' OR stream_id < ?)
ORDER BY stream_id DESC
LIMIT ?;

-- name: ListSessionProjectionItemsBeforeCursorByStatuses :many
SELECT stream_id, repo_path, remote_url, branch, public_state
FROM session_projection
WHERE ((? = '') OR (',' || ? || ',') LIKE '%,' || public_state || ',%')
  AND (? = '' OR stream_id > ?)
ORDER BY stream_id ASC
LIMIT ?;

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

-- name: ListReusableSessionProjectionRefs :many
SELECT *
FROM session_projection
WHERE public_state = 'completed'
  AND repo_path = ?
  AND backend_id = ?
ORDER BY updated_at DESC;
