-- name: CreateSession :one
INSERT INTO sessions (
  id,
  simple_id,
  status,
  backend_id,
  repo_path,
  worktree_path,
  agent_config,
  error
) VALUES (
  ?,
  ?,
  ?,
  ?,
  ?,
  ?,
  ?,
  ?
)
RETURNING *;

-- name: GetSessionByID :one
SELECT *
FROM sessions
WHERE id = ?;

-- name: GetSessionBySimpleID :one
SELECT *
FROM sessions
WHERE simple_id = ?
  AND status IN ('queued', 'running', 'completed');

-- name: GetSessionBySimpleIDAnyStatus :one
SELECT *
FROM sessions
WHERE simple_id = ?;

-- name: ListSessions :many
SELECT *
FROM sessions
ORDER BY updated_at DESC;

-- name: ListSessionsByStatus :many
SELECT *
FROM sessions
WHERE status = ?
ORDER BY updated_at DESC;

-- name: ListRunningSessionIDs :many
SELECT simple_id
FROM sessions
WHERE status = 'running'
ORDER BY updated_at DESC;

-- name: UpdateSessionStatusByID :one
UPDATE sessions
SET status = ?,
    error = ?,
    updated_at = CURRENT_TIMESTAMP
WHERE id = ?
RETURNING *;

-- name: UpdateSessionStatusBySimpleID :one
UPDATE sessions
SET status = ?,
    error = ?,
    updated_at = CURRENT_TIMESTAMP
WHERE simple_id = ?
  AND status IN ('queued', 'running', 'completed')
RETURNING *;

-- name: DeleteSessionByID :exec
DELETE FROM sessions
WHERE id = ?;
